import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { test, expect, type Page } from "@playwright/test";
import matter from "gray-matter";
import { normalizeStep, runStep, type ShotStep } from "./shotsteps.mjs";
import { planMasks, planStandingMasks, shootBaseline, type MaskEntry } from "./shotmasklib.mjs";

// The `fleet-history` shot must capture the same pixels whatever the lab was
// doing when it was shot (#2707, #2802). Two things about the Activity tab follow
// the lab, not the console:
//
//   - the health history's row count. The lab opens with the room commissioning
//     for about a second before it reads healthy, and stretchesOf keeps that
//     stretch only if it outlasted half a second, so a fast host lists one row and
//     a slow one two, and the events and logs below move with it;
//   - the alarm marker's x, the raise's share of a window that runs from the lab's
//     first record to now, so it moves with the lab's age.
//
// These tests run the steps and masks the page DECLARES (read from its
// frontmatter, never copied here) against markup shaped like the Activity tab,
// once per variation, and require the same baseline bytes. No server needed.

const PAGE = fileURLToPath(new URL("../../docs/src/content/docs/operator/fleet.md", import.meta.url));
const shot = ((matter(readFileSync(PAGE, "utf8")).data.screenshots ?? []) as { id: string; steps?: ShotStep[]; mask?: MaskEntry[] }[]).find(
  (s) => s.id === "fleet-history",
);
if (!shot) throw new Error("fleet.md declares no screenshot fleet-history");
const steps = shot.steps ?? [];
const masks = shot.mask ?? [];

const LIST = "[data-testid=stretch-list] > li";

// SystemDetail.tsx's Activity tab, reduced to the parts that move: the ongoing
// stretch first, then (when the lab's first second was long enough to keep it) the
// commissioning stretch, the marker row under the strip, and the events and logs
// that follow the list.
const activity = (opts: { commissioning: boolean; markerAt: number }) => `
<style>
  body { margin: 0; padding: 16px; width: 900px; box-sizing: border-box; background: #0c1320; color: #cbd5e1; font: 14px/20px sans-serif; }
  .card { border: 1px solid #1f2937; border-radius: 8px; padding: 12px; margin: 8px 0; }
  .og-statestrip { height: 10px; background: #14532d; }
  ul { list-style: none; margin: 0; padding: 0; }
  li { border: 1px solid #7f1d1d; border-radius: 8px; margin: 8px 0; padding: 8px 12px; }
  .tabular-nums { font-variant-numeric: tabular-nums; }
</style>
<div><div><span data-testid="since-line">since Nov 27, 03:44 PM · 2m</span></div></div>
<div role="tablist"><button role="tab">Overview</button><button role="tab">Activity</button></div>
<div data-testid="uptime-kpi" class="card">99.9%</div>
<div class="card">
  <div class="og-statestrip"></div>
  <div aria-label="Alarm raises on this timeline" style="position: relative; height: 12px; margin-top: 4px">
    <span data-testid="alarm-marker" style="position: absolute; top: 0; left: ${opts.markerAt}%; color: #ef4444; font-size: 8px; line-height: 12px">▲</span>
    <span style="position: absolute; right: 12px; top: 0; font-size: 10px; line-height: 12px">▲ alarm raised</span>
  </div>
</div>
<div>Health history</div>
<ul data-testid="stretch-list">
  <li data-testid="stretch-0">
    <button type="button" onclick="document.getElementById('why').hidden = !document.getElementById('why').hidden">Degraded · ongoing</button>
    <span class="tabular-nums">Nov 27, 03:44 PM → now · 3m</span>
    <div id="why" hidden><ul><li>Reachability lost dsp-1 Nov 27, 03:44 PM → ongoing</li></ul></div>
  </li>
  ${opts.commissioning ? `<li data-testid="stretch-1"><button type="button">Commissioning</button> <span class="tabular-nums">Nov 27, 03:43 PM → Nov 27, 03:43 PM · 1s</span></li>` : ""}
</ul>
<div data-testid="events-tab"><div>Events</div><ul><li><span class="tabular-nums">4m ago</span> Reachability lost</li></ul></div>
<div data-testid="logs-tab" style="height: 120px">logs</div>`;

const baseline = async (page: Page, opts: { commissioning: boolean; markerAt: number }, only?: { steps?: ShotStep[]; masks?: MaskEntry[] }) => {
  await page.setContent(activity(opts));
  for (const step of only?.steps ?? steps) await runStep(page, "fleet-history", step);
  const plan = [...(await planStandingMasks(page)), ...(await planMasks(page, only?.masks ?? masks))];
  return shootBaseline(page, plan);
};

const FAST = { commissioning: false, markerAt: 12 };
const SLOW = { commissioning: true, markerAt: 41 };

test.describe("the fleet-history shot holds still as the lab ages", () => {
  test("a baseline is the same with or without the commissioning row, and wherever the marker sits", async ({ page }) => {
    const fast = await baseline(page, FAST);
    const slow = await baseline(page, SLOW);
    expect(fast.equals(slow)).toBe(true);
  });

  test("the row count is what moves everything below it: without the hide step the two lab ages differ", async ({ page }) => {
    const unpinned = steps.filter((s) => s.action !== "hide" && s.action !== "expect-count");
    const fast = await baseline(page, { ...FAST, markerAt: 12 }, { steps: unpinned });
    const slow = await baseline(page, { ...FAST, commissioning: true }, { steps: unpinned });
    expect(fast.equals(slow)).toBe(false);
  });

  test("the marker's x is what moves the strip's caption row: without its mask the two lab ages differ", async ({ page }) => {
    const unmasked = masks.filter((m) => !(typeof m === "string" && m.includes("Alarm raises")));
    const young = await baseline(page, { ...FAST, markerAt: 12 }, { masks: unmasked });
    const old = await baseline(page, { ...FAST, markerAt: 41 }, { masks: unmasked });
    expect(young.equals(old)).toBe(false);
  });

  test("a list that still shows other than one row is refused by name, not written", async ({ page }) => {
    await page.setContent(activity(SLOW));
    const guard = steps.find((s) => s.action === "expect-count");
    if (!guard) throw new Error("fleet-history declares no expect-count step");
    await expect(runStep(page, "fleet-history", guard)).rejects.toThrow(`fleet-history: expected 1 visible ${LIST}, found 2`);
    for (const s of steps.filter((s) => s.action === "hide")) await runStep(page, "fleet-history", s);
    await runStep(page, "fleet-history", guard);
  });

  test("a step names a known action and what it needs", () => {
    expect(() => normalizeStep({ action: "scroll", selector: "x" })).toThrow("action must be one of");
    expect(() => normalizeStep({ action: "hide" })).toThrow("hide needs a selector");
    expect(() => normalizeStep({ action: "expect-count", selector: "x" })).toThrow("whole-number count");
    expect(normalizeStep({ action: "expect-count", selector: "x", count: 0 }).count).toBe(0);
  });
});
