import { test, type BrowserContext, type Page } from "@playwright/test";
import { appendFileSync, readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { ROUTE_MANIFEST } from "../src/lib/routemanifest";
import { locate, parseWalkthrough } from "./walkthroughlib.mjs";

// The probe (#965): drive every console route and replay every walkthrough at
// machine speed against a running stack, recording what each surface did as
// raw events (digestlib.mjs documents the shapes) in OG_PROBE_OUT, one JSON
// object per line. It asserts nothing itself: a probe run is evidence, and
// digest.mjs reduces it to the digest an agent reads. Run by web/e2e/probe.sh
// on the walkthrough stack; skipped when OG_PROBE_OUT is unset, so the e2e
// tier never runs it by accident.
//
// Routes come first and walkthroughs after, since a walkthrough may change
// what a route shows (one renames a place).
const OUT = process.env.OG_PROBE_OUT;
const USER = process.env.OG_E2E_USER;
const PASSWORD = process.env.OG_E2E_PASSWORD;
const WALKTHROUGHS = process.env.OG_PROBE_DIR ?? join(process.cwd(), "e2e", "walkthroughs");
const AXE = join(process.cwd(), "node_modules", "axe-core", "axe.min.js");
const STEP_TIMEOUT = 10_000;
// Every protected route (sign-in itself is exercised by the sign-in), then
// every walkthrough.
const ROUTES = process.env.OG_PROBE_ONLY ? [] : ROUTE_MANIFEST.filter((r) => r.auth !== false);
// OG_PROBE_WALKTHROUGHS=0 skips them, for a fleet they were not written
// against (a fresh install has no places to drill into); OG_PROBE_ONLY=<id>
// replays that one walkthrough and visits no route, how a repro is checked.
const ONLY = process.env.OG_PROBE_ONLY;
const SCRIPTS = process.env.OG_PROBE_WALKTHROUGHS === "0" ? [] : readdirSync(WALKTHROUGHS).filter((f) => f.endsWith(".yaml") && (!ONLY || f === `${ONLY}.yaml`)).sort();
// A beat after each action, as a person leaves: at no pause at all a key can
// land before the drawer it should close has finished opening, which is the
// harness racing the console rather than a defect in it.
const SETTLE_MS = 300;

// Web vitals, observed from the first paint: the largest contentful paint and
// the cumulative layout shift, read back when the surface is done.
const VITALS = `(() => {
  const v = { lcp: 0, cls: 0 };
  window.__ogVitals = v;
  try {
    new PerformanceObserver((l) => { for (const e of l.getEntries()) v.lcp = Math.max(v.lcp, e.startTime); })
      .observe({ type: 'largest-contentful-paint', buffered: true });
    new PerformanceObserver((l) => { for (const e of l.getEntries()) if (!e.hadRecentInput) v.cls += e.value; })
      .observe({ type: 'layout-shift', buffered: true });
  } catch {}
})();`;

function emit(event: Record<string, unknown>) {
  appendFileSync(OUT as string, JSON.stringify(event) + "\n");
}

// Every console error, page error and API response a page sees, tagged with
// the surface it was seen on. A miss face (a parametrized route smoked through
// the well-formed unknown id, as routes.spec.ts does) is expected to 404, so
// on one the 404s and the browser's resource-load line for them are not
// recorded; everything else is.
function watch(page: Page, surface: string, miss = false) {
  page.on("pageerror", (e) => emit({ kind: "pageerror", surface, text: e.message }));
  page.on("console", (m) => {
    if (m.type() !== "error") return;
    if (miss && m.text().startsWith("Failed to load resource") && m.text().includes("404")) return;
    emit({ kind: "console", surface, text: m.text() });
  });
  page.on("requestfinished", async (req) => {
    if (!req.url().includes("/api/")) return;
    const res = await req.response().catch(() => null);
    if (miss && res?.status() === 404) return;
    const t = req.timing();
    emit({ kind: "response", surface, method: req.method(), url: req.url(), status: res?.status() ?? 0, ms: t.responseEnd >= 0 ? Math.round(t.responseEnd) : undefined });
  });
  page.on("requestfailed", (req) => {
    if (req.url().includes("/api/")) emit({ kind: "response", surface, method: req.method(), url: req.url(), status: 0 });
  });
}

// The surface's vitals and its accessibility violations, once it settled.
async function settle(page: Page, surface: string) {
  await page.waitForLoadState("networkidle", { timeout: STEP_TIMEOUT }).catch(() =>
    emit({ kind: "console", surface, text: `never settled: the network was still busy after ${STEP_TIMEOUT / 1000}s` }),
  );
  const vitals = await page.evaluate(() => (window as unknown as { __ogVitals?: { lcp: number; cls: number } }).__ogVitals ?? null).catch(() => null);
  if (vitals) emit({ kind: "vitals", surface, lcp: Math.round(vitals.lcp), cls: Number(vitals.cls.toFixed(4)) });
  try {
    await page.addScriptTag({ path: AXE });
    const violations = await page.evaluate(async () => {
      const axe = (window as unknown as { axe: { run: (c: Document, o: unknown) => Promise<{ violations: { id: string; impact: string; help: string; nodes: unknown[] }[] }> } }).axe;
      const r = await axe.run(document, { runOnly: { type: "tag", values: ["wcag2a", "wcag2aa"] } });
      return r.violations.map((v) => ({ id: v.id, impact: v.impact, help: v.help, nodes: v.nodes.length }));
    });
    for (const v of violations) emit({ kind: "axe", surface, ...v });
  } catch (e) {
    emit({ kind: "console", surface: "probe", text: `axe did not run on ${surface}: ${(e as Error).message}` });
  }
}

// Not serial: a surface that fails or times out must never skip the ones
// after it. The run is one worker (probe.sh), so they still go in order.
test.describe("probe", () => {
  test.skip(!OUT || !USER || !PASSWORD, "set OG_PROBE_OUT, OG_E2E_USER and OG_E2E_PASSWORD (run via web/e2e/probe.sh)");

  let ctx: BrowserContext;
  test.beforeAll(async ({ browser }) => {
    ctx = await browser.newContext();
    const page = await ctx.newPage();
    await page.goto("/web/login");
    await page.locator("#login-username").fill(USER as string);
    await page.locator("#login-password").fill(PASSWORD as string);
    await page.getByRole("button", { name: /sign in/i }).click();
    await page.waitForURL((url) => !url.pathname.endsWith("/login"));
    await page.close();
    await ctx.addInitScript(VITALS);
  });

  test("plan", () => {
    // How many surfaces this run means to drive, so the digest can name a
    // hole when fewer were (digestlib.mjs, probe-incomplete).
    emit({ kind: "plan", surface: "probe", surfaces: ROUTES.length + SCRIPTS.length });
  });
  test.afterAll(async () => ctx?.close());

  for (const r of ROUTES) {
    test(`route ${r.smoke}`, async () => {
      const surface = `route:${r.path}`;
      const page = await ctx.newPage();
      watch(page, surface, r.path.includes(":"));
      emit({ kind: "visit", surface });
      try {
        await page.goto("/web" + r.smoke, { waitUntil: "load", timeout: STEP_TIMEOUT });
        await settle(page, surface);
      } catch (e) {
        emit({ kind: "pageerror", surface, text: `route did not load: ${(e as Error).message}` });
      } finally {
        await page.close();
      }
    });
  }

  for (const file of SCRIPTS) {
    const id = file.replace(/\.yaml$/, "");
    test(`walkthrough ${id}`, async () => {
      const surface = `walkthrough:${id}`;
      const w = parseWalkthrough(readFileSync(join(WALKTHROUGHS, file), "utf8"), file);
      // Room for every step's own wait, so a slow step is the walkthrough's
      // recorded failure rather than the test runner's timeout.
      test.setTimeout(30_000 + w.steps.reduce((t: number, s: { timeout?: number }) => t + (s.timeout ?? STEP_TIMEOUT), 0));
      const page = await ctx.newPage();
      watch(page, surface);
      emit({ kind: "visit", surface });
      let failed: { step: number; reason: string; still: string } | null = null;
      for (const step of w.steps) {
        try {
          await replay(page, step);
          if (step.kind !== "wait") await page.waitForTimeout(SETTLE_MS);
          // A replay keeps a still per step, so a passing script still shows
          // the state it reached (probe.sh replay sets this; a full probe does not).
          if (process.env.OG_PROBE_STILLS === "1") await page.screenshot({ path: join(dirname(OUT as string), `${id}-step-${step.n}.png`) }).catch(() => {});
        } catch (e) {
          // The error's first lines carry Playwright's call log (what it
          // waited on and why it never acted); the frame it failed on is kept
          // beside the digest for a reader who needs to see it.
          const reason = (e as Error).message.split("\n").map((l) => l.replace(/\u001b\[[0-9;]*m/g, "").trim()).filter(Boolean).slice(0, 4).join(" | ");
          const still = `${id}-step-${step.n}-failed.png`;
          await page.screenshot({ path: join(dirname(OUT as string), still) }).catch(() => {});
          failed = { step: step.n, reason, still };
          break;
        }
      }
      if (!failed) await settle(page, surface);
      emit({ kind: "walkthrough", surface, ok: !failed, ...(failed ?? {}) });
      await page.close();
    });
  }
});

// One walkthrough step at machine speed: the recorder's actions without its
// pacing, cursor or captions.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
async function replay(page: Page, step: any) {
  switch (step.kind) {
    case "goto":
      await page.goto("/web" + step.path, { waitUntil: "load", timeout: STEP_TIMEOUT });
      return;
    case "press":
      await page.keyboard.press(step.key);
      return;
    case "wait":
      await locate(page, step.target).filter({ visible: true }).first().waitFor({ state: "visible", timeout: step.timeout });
      return;
  }
  const el = locate(page, step.target);
  if (step.kind === "click") await el.click({ timeout: STEP_TIMEOUT });
  else if (step.kind === "type") {
    await el.click({ timeout: STEP_TIMEOUT });
    await el.pressSequentially(step.text, { timeout: STEP_TIMEOUT });
  } else await el.selectOption(step.value, { timeout: STEP_TIMEOUT });
}
