// The walkthrough recorder (#924). Run inside the Playwright container on the
// lab's network (web/e2e/walkthroughs.sh, via lab_pw):
//
//   node e2e/walkthrough.mjs record <file.yaml> <out-dir>
//       make the script's setup through the API (off camera), sign in, play the
//       script against the real console with Playwright's
//       video capture on, and write <out-dir>/<id>.webm, a still per step
//       (step-NN.png), poster.png and walkthrough.json. A step whose expected
//       result never appears fails the run naming the step, keeping the stills
//       taken so far (and the failing frame as step-NN-failed.png).
//   node e2e/walkthrough.mjs budget <out-dir>/<id>.mp4 <seconds> <opening-ymax>
//       the budget gate on the converted mp4 and the animated preview encoded
//       beside it (preview.webp): within MAX_SECONDS, MAX_BYTES and
//       MAX_PREVIEW_BYTES (walkthroughlib.mjs), else fail naming which; a
//       duration ffprobe never measured, a missing preview, or one whose
//       opening frame (its brightest luma, <opening-ymax>) is not the painted
//       title card, fails too. A
//       pass adds a budget mark ({ seconds, bytes, previewBytes }) and the
//       preview's name to walkthrough.json, so a manifest without the mark
//       never passed. OG_WALKTHROUGH_MAX_SECONDS / OG_WALKTHROUGH_MAX_BYTES /
//       OG_WALKTHROUGH_MAX_PREVIEW_BYTES may lower the limits (never raise
//       them), for the recorder's own test only.
//
// What the viewer sees is drawn into the page by an init script, in a shadow
// root above everything (pointer-events none, so it never takes a click): a
// cursor that glides to each target before acting and ripples where it clicks,
// a highlight ring on the target, and a caption bar at the bottom carrying the
// step's caption. A title card (title and description) opens the video. The
// pace is a person's: a beat to read the caption, the glide, the action, a beat
// to see its result; typing is keyed at a readable speed.
//
// Env: OG_E2E_BASE, OG_E2E_USER, OG_E2E_PASSWORD, and OG_TOKEN for a script's
// setup (set by lab_pw).
import { chromium } from '@playwright/test';
import { copyFile, mkdir, readFile, readdir, rename, stat, unlink, writeFile } from 'node:fs/promises';
import { basename, dirname, join } from 'node:path';
import { PREVIEW } from './walkthroughcore.mjs';
import {
  budgetLimits,
  budgetProblems,
  describeTarget,
  idProblem,
  locate,
  parseWalkthrough,
  previewOpeningProblem,
  runSetup,
  previewStartSeconds,
  stepFailure,
  stillName,
} from './walkthroughlib.mjs';

const VIEWPORT = { width: 1320, height: 860 };
// The pace, in milliseconds.
const PACE = {
  title: 2200, // the title card
  read: 700, // the caption shows before the cursor moves
  glide: 650, // the cursor's travel to a target
  settle: 250, // on the target, before acting
  after: 1000, // after an action, to see its result
  key: 110, // per typed character
  aside: 300, // the cursor stepping off a field it typed into
  load: 600, // after a navigation, for the page to settle
};
const ACT_TIMEOUT = 10_000;

async function budget(mp4, seconds, openingYmax) {
  const id = basename(mp4, '.mp4');
  const { size: bytes } = await stat(mp4);
  const previewBytes = await stat(join(dirname(mp4), PREVIEW)).then((s) => s.size, () => undefined);
  const limits = budgetLimits(process.env);
  const problems = budgetProblems(id, { seconds, bytes, previewBytes }, limits);
  const opening = previewOpeningProblem(id, openingYmax);
  if (opening) problems.push(opening);
  for (const p of problems) console.error(p);
  // The pass is recorded in the manifest, and only here: a manifest with no
  // budget mark never came from a recording that passed, so the docs copy
  // (docs/scripts/copy-walkthroughs.mjs) refuses it whatever failed.txt says.
  // A failure takes back a mark an earlier pass on the same files left.
  const manifest = join(dirname(mp4), 'walkthrough.json');
  const m = JSON.parse(await readFile(manifest, 'utf8'));
  if (problems.length) {
    if ('budget' in m || 'preview' in m) {
      delete m.budget;
      delete m.preview;
      await writeFile(manifest, JSON.stringify(m, null, 2) + '\n');
    }
    return 1;
  }
  m.preview = PREVIEW;
  m.budget = { seconds: Number(seconds.toFixed(1)), bytes, previewBytes };
  await writeFile(manifest, JSON.stringify(m, null, 2) + '\n');
  const mb = (b) => (b / 1e6).toFixed(2);
  console.log(
    `walkthrough ${id}: ${seconds.toFixed(1)}s, ${mb(bytes)} MB, preview ${mb(previewBytes)} MB ` +
      `(budget: under ${limits.maxSeconds}s, under ${mb(limits.maxBytes)} MB, preview under ${mb(limits.maxPreviewBytes)} MB)`,
  );
  return 0;
}

async function record(file, out) {
  const id = basename(file, '.yaml');
  const bad = idProblem(id);
  if (bad) {
    console.error(bad);
    return 2;
  }
  // Cleared before anything can fail, so a refused script never leaves the
  // previous run's recording standing as though it were this one's.
  await mkdir(out, { recursive: true });
  await clearOutputs(out);
  let w;
  try {
    w = parseWalkthrough(await readFile(file, 'utf8'), file);
  } catch (e) {
    console.error(e.message);
    return 1;
  }
  const base = process.env.OG_E2E_BASE;
  const user = process.env.OG_E2E_USER;
  const password = process.env.OG_E2E_PASSWORD;
  if (!base || !user || !password) {
    console.error('walkthrough: set OG_E2E_BASE, OG_E2E_USER and OG_E2E_PASSWORD (run via make walkthroughs)');
    return 2;
  }

  try {
    await runSetup(w.setup, { base, token: process.env.OG_TOKEN, fetch });
  } catch (e) {
    console.error(`walkthrough ${id}: ${e.message}`);
    return 1;
  }

  const browser = await chromium.launch();
  try {
    // Sign in off camera, then record in a fresh context holding the session.
    const storageState = await signIn(browser, base, user, password);
    const ctx = await browser.newContext({
      viewport: VIEWPORT,
      colorScheme: 'dark',
      storageState,
      recordVideo: { dir: out, size: VIEWPORT },
    });
    await ctx.addInitScript(OVERLAY);
    // The video starts with the page, before the title card is painted; the
    // preview starts after it is (previewStartSeconds), so it never opens on
    // the blank lead-in. Taken before the page opens, so the start errs late.
    const videoStart = Date.now();
    const page = await ctx.newPage();
    const cursor = { x: VIEWPORT.width / 2, y: VIEWPORT.height / 2 };

    await page.setContent(await titleCard(w));
    const faces = await page.evaluate(async () => {
      const want = ['600 46px "IBM Plex Sans"', '400 21px "IBM Plex Sans"'];
      await Promise.all(want.map((f) => document.fonts.load(f).catch(() => {})));
      return want.filter((f) => !document.fonts.check(f));
    });
    if (faces.length) console.error(`walkthrough ${id}: the title card fell back from ${faces.join(', ')}`);
    // Two animation frames after the faces load, the card is on screen.
    await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
    const previewStart = previewStartSeconds(videoStart, Date.now());
    await page.waitForTimeout(PACE.title);

    let failure = null;
    const stills = [];
    for (const step of w.steps) {
      // The phase the step is in, kept current by play(), so a failure names
      // what was being waited on and that wait's own timeout.
      const phase = { name: 'act', timeoutMs: ACT_TIMEOUT };
      try {
        await play(page, base, step, w.steps.length, cursor, phase);
        Object.assign(phase, { name: 'still', timeoutMs: null });
        await page.waitForTimeout(PACE.after);
        const name = stillName(step.n);
        await page.screenshot({ path: join(out, name) });
        stills.push(name);
        await page.waitForTimeout(step.hold);
      } catch (e) {
        failure = stepFailure(id, step, phase, e?.message ?? e);
        await page.screenshot({ path: join(out, stillName(step.n).replace('.png', '-failed.png')) }).catch(() => {});
        break;
      }
    }
    await overlay(page, 'caption', null);
    const video = page.video();
    await ctx.close();
    const webm = join(out, `${id}.webm`);
    await rename(await video.path(), webm);

    if (failure) {
      console.error(failure);
      console.error(`walkthrough ${id}: kept ${stills.length} still(s) in ${out}`);
      return 1;
    }
    // The poster is the payoff the walkthrough builds to, with its caption:
    // the step marked poster: true, else the final state.
    await copyFile(join(out, stillName(w.poster)), join(out, 'poster.png'));
    await writeFile(
      join(out, 'walkthrough.json'),
      JSON.stringify(
        { id, title: w.title, description: w.description, steps: w.steps.map((s, i) => ({ n: s.n, kind: s.kind, caption: s.caption, still: stills[i] })), poster: stillName(w.poster) },
        null,
        2,
      ) + '\n',
    );
    // Where the preview encode (walkthroughs.sh) seeks to, in seconds.
    await writeFile(join(out, 'preview-start.txt'), `${previewStart}\n`);
    console.log(`walkthrough ${id}: recorded ${w.steps.length} steps`);
    return 0;
  } finally {
    await browser.close();
  }
}

// The files a run writes, cleared first so a shorter script never leaves a
// previous run's stills behind. Only the recorder's own outputs are touched.
async function clearOutputs(out) {
  for (const f of await readdir(out)) {
    if (/^step-\d+(-failed)?\.png$|^poster\.png$|^preview\.webp$|^preview-start\.txt$|^walkthrough\.json$|\.webm$|\.mp4$/.test(f)) await unlink(join(out, f));
  }
}

async function signIn(browser, base, user, password) {
  const ctx = await browser.newContext({ viewport: VIEWPORT, colorScheme: 'dark' });
  const page = await ctx.newPage();
  await page.goto(`${base}/web/login`);
  await page.locator('#login-username').fill(user);
  await page.locator('#login-password').fill(password);
  await page.getByRole('button', { name: /sign in/i }).click();
  await page.waitForURL((u) => !u.pathname.endsWith('/login'));
  const state = await ctx.storageState();
  await ctx.close();
  return state;
}

// One step: the caption first, then the action with the cursor drawn to it.
// Each phase is recorded in `phase` (its name and timeout) before it starts.
async function play(page, base, step, total, cursor, phase) {
  const at = (name, timeoutMs) => Object.assign(phase, { name, timeoutMs });
  await overlay(page, 'ring', null);
  await overlay(page, 'caption', { text: step.caption, n: step.n, total });
  await page.waitForTimeout(PACE.read);
  switch (step.kind) {
    case 'goto':
      at('load', ACT_TIMEOUT);
      await page.goto(`${base}/web${step.path}`, { waitUntil: 'load', timeout: ACT_TIMEOUT });
      await overlay(page, 'caption', { text: step.caption, n: step.n, total });
      await overlay(page, 'cursor', { ...cursor, ms: 0 });
      await page.waitForTimeout(PACE.load);
      return;
    case 'press':
      at('act', null);
      await page.keyboard.press(step.key);
      return;
    case 'wait': {
      // Any visible match will do: a result can render in more than one place.
      const el = locate(page, step.target).filter({ visible: true }).first();
      at('visible', step.timeout);
      await el.waitFor({ state: 'visible', timeout: step.timeout });
      await point(page, el, cursor);
      return;
    }
  }
  // click, type, select: a single target (strict), visible, scrolled to, glided to.
  const el = locate(page, step.target);
  at('visible', ACT_TIMEOUT);
  await el.waitFor({ state: 'visible', timeout: ACT_TIMEOUT });
  at('scroll', ACT_TIMEOUT);
  await el.scrollIntoViewIfNeeded({ timeout: ACT_TIMEOUT });
  const box = await el.boundingBox();
  if (!box) throw new Error(`${describeTarget(step.target)} has no box on screen`);
  const to = { x: box.x + box.width / 2, y: box.y + box.height / 2 };
  if (step.kind !== 'click') to.x = Math.min(box.x + 24, to.x);
  await overlay(page, 'ring', box);
  await overlay(page, 'cursor', { ...to, ms: PACE.glide });
  await page.mouse.move(to.x, to.y, { steps: 12 });
  await page.waitForTimeout(PACE.glide + PACE.settle);
  Object.assign(cursor, to);
  await overlay(page, 'ripple', to);
  at('act', ACT_TIMEOUT);
  if (step.kind === 'click') {
    await el.click({ timeout: ACT_TIMEOUT });
  } else if (step.kind === 'type') {
    await el.click({ timeout: ACT_TIMEOUT });
    // Step off the field so the text being typed stays in view.
    Object.assign(cursor, { y: box.y + box.height + 18 });
    await overlay(page, 'cursor', { ...cursor, ms: PACE.aside });
    await page.waitForTimeout(PACE.aside);
    at('act', ACT_TIMEOUT + step.text.length * PACE.key);
    await el.pressSequentially(step.text, { delay: PACE.key, timeout: ACT_TIMEOUT + step.text.length * PACE.key });
  } else {
    await el.selectOption(step.value, { timeout: ACT_TIMEOUT });
  }
  await overlay(page, 'ring', null);
}

// A result a wait found: ringed, and the cursor glides to its right edge (the
// overlay cursor only, so nothing hovers), pointing at it without covering it
// or resting on a control the viewer might think was used.
async function point(page, el, cursor) {
  const box = await el.boundingBox();
  if (!box) return;
  await overlay(page, 'ring', box);
  // The part of the result on screen: a tall one runs past the viewport.
  const top = Math.max(box.y, 0);
  const bottom = Math.min(box.y + box.height, VIEWPORT.height);
  Object.assign(cursor, { x: Math.min(box.x + box.width, VIEWPORT.width) - 20, y: (top + bottom) / 2 });
  await overlay(page, 'cursor', { ...cursor, ms: PACE.glide });
  await page.waitForTimeout(PACE.glide);
}

// Drives the in-page overlay. A navigation can race it, so a missed call is
// re-sent once the new document's overlay exists.
async function overlay(page, what, arg) {
  for (let i = 0; i < 2; i++) {
    try {
      await page.evaluate(([w, a]) => window.__walkthrough?.[w](a), [what, arg]);
      return;
    } catch {
      await page.waitForLoadState('domcontentloaded').catch(() => {});
    }
  }
}

const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]);

// The title card: its own document on the console's dark ground, in the
// console's typeface, embedded from the vendored faces (web/public/fonts) so
// the card needs nothing from the network. Run from web/, as the container is.
async function titleCard(w) {
  const face = async (weight) => {
    const b64 = (await readFile(`public/fonts/ibm-plex-sans-latin-${weight}.woff2`)).toString('base64');
    return `@font-face{font-family:"IBM Plex Sans";font-weight:${weight};font-display:block;src:url(data:font/woff2;base64,${b64}) format("woff2")}`;
  };
  return `<!doctype html><html><head><style>
  ${await face(400)}${await face(600)}
  html,body{margin:0;height:100%;background:#080c16;color:#e6edf6;font-family:"IBM Plex Sans",ui-sans-serif,system-ui,sans-serif}
  .c{height:100%;display:flex;flex-direction:column;justify-content:center;padding:0 120px;box-sizing:border-box}
  .e{color:#21cab9;font-size:15px;font-weight:600;letter-spacing:.16em;text-transform:uppercase;margin-bottom:22px}
  h1{font-size:46px;line-height:1.15;font-weight:600;margin:0 0 20px;max-width:960px}
  p{font-size:21px;line-height:1.5;color:#a9b4c4;margin:0;max-width:880px}
  .r{width:72px;height:4px;background:#21cab9;border-radius:2px;margin-top:36px}
  </style></head><body><div class="c"><div class="e">Omniglass walkthrough</div><h1>${esc(w.title)}</h1>${
    w.description ? `<p>${esc(w.description)}</p>` : ''
  }<div class="r"></div></div></body></html>`;
}

// The overlay, installed in every document the recording loads. It lives in a
// shadow root on <html> (outside <body>, which the app owns), so the console's
// styles never reach it and it never reaches them.
const OVERLAY = () => {
  if (window.__walkthrough) return;
  const install = () => {
    const host = document.createElement('div');
    host.setAttribute('data-walkthrough-overlay', '');
    host.style.cssText = 'position:fixed;inset:0;pointer-events:none;z-index:2147483647';
    const root = host.attachShadow({ mode: 'open' });
    root.innerHTML = `<style>
      *{box-sizing:border-box}
      .cur{position:fixed;left:0;top:0;width:26px;height:26px;margin:-13px 0 0 -13px;border-radius:50%;
        background:rgba(255,255,255,.92);border:3px solid #080c16;box-shadow:0 0 0 2px #21cab9,0 4px 14px rgba(0,0,0,.55);
        opacity:0;transition-property:transform;transition-timing-function:cubic-bezier(.4,0,.2,1)}
      .rip{position:fixed;width:26px;height:26px;margin:-13px 0 0 -13px;border-radius:50%;border:3px solid #21cab9;
        animation:rip .7s ease-out forwards}
      @keyframes rip{from{transform:scale(1);opacity:1}to{transform:scale(3.4);opacity:0}}
      .ring{position:fixed;border:2px solid #21cab9;border-radius:8px;box-shadow:0 0 0 4px rgba(33,202,185,.22);
        opacity:0;transition:opacity .2s}
      .cap{position:fixed;left:50%;bottom:28px;transform:translateX(-50%);max-width:1080px;min-width:360px;
        display:flex;align-items:center;gap:16px;padding:14px 22px;border-radius:12px;
        background:rgba(4,7,14,.9);border:1px solid rgba(33,202,185,.45);box-shadow:0 8px 28px rgba(0,0,0,.5);
        color:#f2f6fb;font:500 20px/1.4 "IBM Plex Sans",ui-sans-serif,system-ui,sans-serif;
        opacity:0;transition:opacity .25s}
      .n{flex:none;color:#21cab9;font:600 14px/1 "JetBrains Mono",ui-monospace,monospace;letter-spacing:.04em}
    </style><div class="ring"></div><div class="cap"><span class="n"></span><span class="t"></span></div><div class="cur"></div>`;
    document.documentElement.appendChild(host);
    const cur = root.querySelector('.cur');
    cur.style.transform = `translate(${innerWidth / 2}px,${innerHeight / 2}px)`;
    const ring = root.querySelector('.ring');
    const cap = root.querySelector('.cap');
    window.__walkthrough = {
      cursor({ x, y, ms }) {
        cur.style.transitionDuration = `${ms}ms`;
        cur.style.transform = `translate(${x}px,${y}px)`;
        cur.style.opacity = '1';
      },
      ripple({ x, y }) {
        const r = document.createElement('div');
        r.className = 'rip';
        r.style.left = `${x}px`;
        r.style.top = `${y}px`;
        root.appendChild(r);
        setTimeout(() => r.remove(), 800);
      },
      ring(box) {
        if (!box) {
          ring.style.opacity = '0';
          return;
        }
        const pad = 4;
        Object.assign(ring.style, { left: `${box.x - pad}px`, top: `${box.y - pad}px`, width: `${box.width + 2 * pad}px`, height: `${box.height + 2 * pad}px`, opacity: '1' });
      },
      caption(c) {
        if (!c) {
          cap.style.opacity = '0';
          return;
        }
        cap.querySelector('.n').textContent = `${c.n}/${c.total}`;
        cap.querySelector('.t').textContent = c.text;
        cap.style.opacity = '1';
      },
    };
  };
  if (document.documentElement) install();
  else document.addEventListener('DOMContentLoaded', install, { once: true });
};

// Dispatch last: the definitions above (OVERLAY, a const) must exist first.
const [mode, ...args] = process.argv.slice(2);
if (mode === 'record' && args.length === 2) {
  process.exit(await record(args[0], args[1]));
} else if (mode === 'budget' && args.length === 3) {
  process.exit(await budget(args[0], Number(args[1]), Number(args[2])));
} else {
  console.error('usage: node e2e/walkthrough.mjs record <file.yaml> <out-dir> | budget <file.mp4> <seconds> <opening-ymax>');
  process.exit(2);
}
