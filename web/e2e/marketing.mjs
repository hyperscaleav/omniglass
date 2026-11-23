// @ts-check
// The marketing recorder (#2299): plays web/e2e/marketing/shots.yaml against a
// corporate lab and leaves, per shot, a PNG (a still) or a WebM and a PNG (a
// loop and its poster frame). web/e2e/marketing.sh runs it in a Playwright
// container on the lab network, then encodes the files and writes the manifest.
// The shot list, its validation, the budget and the manifest are in
// marketinglib.mjs, unit tested; this is the thin I/O around them.
//
//   node e2e/marketing.mjs check <shots.yaml> <dest>   before the lab: the list parses,
//                                                      the destination is usable, the
//                                                      omnibot recording is shipped
//   node e2e/marketing.mjs lab-args <shots.yaml>       "<scenario> <systems> <replay>"
//   node e2e/marketing.mjs capture <shots.yaml> <out> [id...]  play the shots, or only those ids
//                                                      (needs the lab's
//                                                      OG_E2E_BASE, OG_TOKEN, OG_E2E_USER,
//                                                      OG_E2E_PASSWORD)
//   node e2e/marketing.mjs budget <id> <seconds> <bytes>   a loop's budget, exit 1 over
//   node e2e/marketing.mjs ids <shots.yaml>            "<id> <kind>" per shot, in order
//   node e2e/marketing.mjs manifest <shots.yaml> <dest> <dims.json>   write shots.json
//
// A failing step fails the run naming the shot, the step, its kind and what it
// waited for, and keeps the frame it failed on beside the captures.

import { existsSync, readFileSync, statSync, writeFileSync, mkdirSync } from 'node:fs';
import { join } from 'node:path';
import {
  CAPTURE_CSS, buildManifest, cameraTail, concatList, retime, budgetProblems, conditionMet, destProblem,
  expandAddress, parseShots, pick, shotFiles, referencedResources,
} from './marketinglib.mjs';
import { describeTarget, locate } from './walkthroughlib.mjs';

const ACT_TIMEOUT = 15_000;
// Loops record at twice the viewport's pixels, so the camera's zoom stays sharp.
// Pacing, in milliseconds: how long the cursor glides to a target before it acts,
// and the delay between typed keys.
const PACE = { glide: 250, key: 35, load: 350 };

/** @param {string} file @returns {import('./marketinglib.mjs').ShotList} */
function readList(file) {
  const { list, problems } = parseShots(readFileSync(file, 'utf8'));
  if (!list) {
    for (const p of problems) console.error(`marketing: ${file}: ${p}`);
    process.exit(2);
  }
  return list;
}

const mode = process.argv[2];
const args = process.argv.slice(3);

async function main() {
  if (mode === 'check') {
    const [file, dest] = args;
    const list = readList(file);
    const problem = destProblem(dest) ?? (!existsSync(dest) || !statSync(dest).isDirectory() ? `MARKETING_DEST "${dest}" is not an existing directory` : null);
    if (problem) {
      console.error(`marketing: ${problem}`);
      process.exit(2);
    }
    const replay = list.lab.omnibotReplay;
    if (replay && !existsSync(new URL(`../../internal/lab/omnibot/${replay}.yaml`, import.meta.url))) {
      console.error(`marketing: omnibot_replay "${replay}" is not a recording the lab ships (internal/lab/omnibot/)`);
      process.exit(2);
    }
    console.log(`marketing: ${list.shots.length} shots, destination ${dest}`);
  } else if (mode === 'lab-args') {
    const l = readList(args[0]).lab;
    console.log(`${l.scenario} ${l.systems} ${l.omnibotReplay}`);
  } else if (mode === 'ids') {
    for (const s of readList(args[0]).shots) console.log(s.id, s.kind);
  } else if (mode === 'budget') {
    const [id, seconds, bytes] = args;
    const problems = budgetProblems(id, { seconds: Number(seconds), bytes: Number(bytes) });
    for (const p of problems) console.error(`marketing: ${p}`);
    process.exit(problems.length ? 1 : 0);
  } else if (mode === 't0') {
    console.log(JSON.parse(readFileSync(args[0], 'utf8'))[args[1]].t0);
  } else if (mode === 'manifest') {
    const [file, dest] = args;
    const list = readList(file);
      const manifest = buildManifest({
      generatedAt: new Date().toISOString(),
      lab: { scenario: list.lab.scenario, systems: list.lab.systems },
      shots: list.shots.map((s) => ({ id: s.id, kind: s.kind, ...shotFiles(s), alt: s.alt, width: s.viewport.width, height: s.viewport.height })),
    });
    writeFileSync(join(dest, 'shots.json'), JSON.stringify(manifest, null, 2) + '\n');
    console.log(`marketing: wrote ${join(dest, 'shots.json')} (${list.shots.length} shots)`);
  } else if (mode === 'capture') {
    await capture(args[0], args[1], args.slice(2));
  } else {
    console.error('usage: marketing.mjs check|lab-args|capture|budget|manifest ...');
    process.exit(2);
  }
}

// ---- capture -----------------------------------------------------------------

/** @param {string} file @param {string} out @param {string[]} only shot ids to play (default: every shot) */
async function capture(file, out, only) {
  const base = process.env.OG_E2E_BASE;
  const token = process.env.OG_TOKEN;
  if (!base || !token) {
    console.error('marketing: capture needs OG_E2E_BASE and OG_TOKEN (the lab sets them)');
    process.exit(2);
  }
  const list = readList(file);
  mkdirSync(out, { recursive: true });
  const api = makeApi(base, token);

  // Before any capture: every component, system and endpoint a setup item or a
  // start names must exist on this lab, so a typo or a fleet that changed fails
  // here, naming the shot, rather than ten minutes into a recording.
  const bad = [];
  for (const [where, items, start] of [
    ['the list', list.setup, null],
    ...list.shots.map((s) => [`shot ${s.id}`, s.setup, s.start]),
  ]) {
    for (const ref of referencedResources(items, start)) {
      const status = await api.status(ref.path);
      if (status === 404) bad.push(`${where}: ${ref.kind} "${ref.id}" is not on this lab`);
    }
  }
  if (bad.length) {
    for (const b of bad) console.error(`marketing: ${b}`);
    process.exit(1);
  }

  const { chromium } = await import('@playwright/test');
  const browser = await chromium.launch();
  try {
    const state = await signIn(browser, base);
    await runSetup(api, 'the list', list.setup);
    // Kept as each shot finishes (and merged over a previous run's), so a run
    // that plays only some shots, or stops at a failure, loses nothing.
    const dimsFile = join(out, 'dims.json');
    const dims = existsSync(dimsFile) ? JSON.parse(readFileSync(dimsFile, 'utf8')) : {};
    for (const shot of list.shots.filter((x) => only.length === 0 || only.includes(x.id))) {
      console.log(`marketing: shot ${shot.id} (${shot.kind})`);
      await runSetup(api, `shot ${shot.id}`, shot.setup);
      dims[shot.id] = await record(browser, base, state, shot, out);
      writeFileSync(dimsFile, JSON.stringify(dims, null, 2));
    }
  } finally {
    await browser.close();
  }
}

/** @param {string} base @param {string} token */
function makeApi(base, token) {
  const headers = { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' };
  const url = (/** @type {string} */ p) => `${base}/api/v1${expandAddress(p)}`;
  return {
    /** @param {string} path @returns {Promise<number>} */
    async status(path) {
      return (await fetch(url(path), { headers })).status;
    },
    /** @param {string} path @returns {Promise<unknown>} */
    async get(path) {
      const r = await fetch(url(path), { headers });
      if (!r.ok) throw new Error(`GET ${path}: ${r.status} ${(await r.text()).slice(0, 200)}`);
      return r.json();
    },
    /** @param {string} method @param {string} path @param {unknown} body */
    async send(method, path, body) {
      const r = await fetch(url(path), { method, headers, body: JSON.stringify(body) });
      if (!r.ok) throw new Error(`${method} ${path}: ${r.status} ${(await r.text()).slice(0, 300)}`);
    },
  };
}

/** @param {ReturnType<typeof makeApi>} api @param {string} where @param {import('./marketinglib.mjs').SetupItem[]} items */
async function runSetup(api, where, items) {
  let n = 0;
  for (const it of items) {
    n++;
    try {
      if (it.kind === 'call') {
        await api.send(it.method, it.path, it.body);
        continue;
      }
      const deadline = Date.now() + it.timeout;
      let last;
      for (;;) {
        last = pick(await api.get(it.get), it.field);
        if (conditionMet(it.equals, last)) break;
        if (Date.now() > deadline) {
          throw new Error(`waited ${it.timeout / 1000}s for ${it.get} ${it.field} to equal ${JSON.stringify(it.equals)}; it read ${JSON.stringify(last)}`);
        }
        await new Promise((r) => setTimeout(r, 3000));
      }
    } catch (e) {
      console.error(`marketing: ${where}: setup item ${n} failed: ${/** @type {Error} */ (e).message}`);
      process.exit(1);
    }
  }
}

/** @param {import('@playwright/test').Browser} browser @param {string} base */
async function signIn(browser, base) {
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  await page.goto(`${base}/web/login`);
  await page.locator('#login-username').fill(process.env.OG_E2E_USER ?? 'dev');
  await page.locator('#login-password').fill(process.env.OG_E2E_PASSWORD ?? 'dev');
  await page.keyboard.press('Enter');
  await page.waitForURL((u) => !u.pathname.endsWith('/login'));
  const state = await ctx.storageState();
  await ctx.close();
  return state;
}

// A cursor the recording draws (a video has none): an arrow that eases toward
// the real pointer, with a ring where it presses.
const CURSOR = () => {
  const el = document.createElement('div');
  el.id = '__og_cursor';
  el.style.cssText = 'position:fixed;left:0;top:0;width:22px;height:22px;z-index:2147483647;pointer-events:none;transform:translate(-100px,-100px);filter:drop-shadow(0 1px 2px rgba(0,0,0,.55))';
  const ns = 'http://www.w3.org/2000/svg';
  const svg = document.createElementNS(ns, 'svg');
  svg.setAttribute('width', '22');
  svg.setAttribute('height', '22');
  svg.setAttribute('viewBox', '0 0 22 22');
  const arrow = document.createElementNS(ns, 'path');
  arrow.setAttribute('d', 'M3 2l14 8.2-6.2 1.6-2.4 6z');
  arrow.setAttribute('fill', '#fff');
  arrow.setAttribute('stroke', '#0b1020');
  arrow.setAttribute('stroke-width', '1.4');
  arrow.setAttribute('stroke-linejoin', 'round');
  svg.append(arrow);
  el.append(svg);
  const ring = document.createElement('div');
  ring.style.cssText = 'position:fixed;left:0;top:0;width:34px;height:34px;margin:-17px 0 0 -17px;border-radius:50%;border:2px solid #21cab9;z-index:2147483646;pointer-events:none;opacity:0';
  let tx = -100, ty = -100, x = -100, y = -100;
  const tick = () => {
    x += (tx - x) * 0.22;
    y += (ty - y) * 0.22;
    el.style.transform = `translate(${x}px,${y}px)`;
    requestAnimationFrame(tick);
  };
  addEventListener('mousemove', (e) => { tx = e.clientX; ty = e.clientY; }, true);
  addEventListener('mousedown', (e) => {
    ring.style.left = `${e.clientX}px`;
    ring.style.top = `${e.clientY}px`;
    ring.animate([{ opacity: 0.9, transform: 'scale(.4)' }, { opacity: 0, transform: 'scale(1.5)' }], { duration: 450, easing: 'ease-out' });
  }, true);
  const mount = () => { document.documentElement.append(ring, el); requestAnimationFrame(tick); };
  if (document.documentElement) mount();
  else addEventListener('DOMContentLoaded', mount);
};

// The capture-only stylesheet, added to every document before the console paints.
const STYLE = (/** @type {string} */ css) => {
  const add = () => {
    if (document.getElementById('__og_capture_css')) return;
    const s = document.createElement('style');
    s.id = '__og_capture_css';
    s.textContent = css;
    (document.head ?? document.documentElement).append(s);
  };
  if (document.documentElement) add();
  document.addEventListener('DOMContentLoaded', add);
};

/**
 * Plays one shot: its steps, then the still or, for a loop, the recording and
 * its poster frame. Returns what the encoder needs.
 * @param {import('@playwright/test').Browser} browser
 * @param {string} base
 * @param {any} storageState
 * @param {import('./marketinglib.mjs').Shot} shot
 * @param {string} out
 */
async function record(browser, base, storageState, shot, out) {
  const { width, height } = shot.viewport;
  const loop = shot.kind === 'loop';
  const ctx = await browser.newContext({
    viewport: { width, height },
    deviceScaleFactor: 2,
    colorScheme: 'dark',
    storageState,
  });
  await ctx.addInitScript(STYLE, CAPTURE_CSS);
  if (loop) await ctx.addInitScript(CURSOR);
  const page = await ctx.newPage();
  const cursor = { x: width / 2, y: height / 2 };
  /** @type {Camera} */
  const cam = { events: [], lapses: [], now: () => 0 };
  let current = 'start';
  try {
    await page.goto(`${base}/web${shot.start}`, { waitUntil: 'load', timeout: ACT_TIMEOUT * 2 });
    if (shot.ready) {
      current = `ready ${describeTarget(shot.ready)}`;
      await locate(page, shot.ready).filter({ visible: true }).first().waitFor({ state: 'visible', timeout: 60_000 });
    }
    await page.waitForTimeout(PACE.load);
    for (const step of shot.prep) {
      current = `prep step ${step.n} (${step.kind}${step.target ? ` ${describeTarget(step.target)}` : ''})`;
      await play(page, base, step, cursor, false, cam);
    }
    // The recording opens here, on the console's first paint: the clip's clock
    // (the camera's too) starts now.
    const t0 = Date.now();
    cam.now = () => (Date.now() - t0) / 1000;
    const frames = loop ? await startFrames(ctx, page, t0) : null;
    if (loop) await page.mouse.move(cursor.x, cursor.y);
    for (const step of shot.steps) {
      current = `step ${step.n} (${step.kind}${step.target ? ` ${describeTarget(step.target)}` : ''})`;
      const t = Date.now();
      await play(page, base, step, cursor, loop, cam);
      if (process.env.MARKETING_TRACE) console.log(`  ${shot.id} ${current}: ${((Date.now() - t) / 1000).toFixed(1)}s`);
    }
    // Let the last state settle and the stills' data finish arriving.
    current = 'final frame';
    await page.waitForTimeout(shot.kind === 'still' ? 1500 : 300);
    await page.screenshot({ path: join(out, `${shot.id}.png`) });
    if (frames) {
      // Keep recording while the camera eases back out, so the clip ends on the
      // whole frame and loops without a jump.
      await page.waitForTimeout(cameraTail(cam.events) * 1000);
      const taken = await frames.stop();
      // Waits played faster are shortened in the frames and in the camera's times
      // alike, so a focus lands on the frame it was logged for.
      const events = cam.events.map((e) => ({ ...e, t: retime(e.t, cam.lapses) }));
      writeFrames(taken.map((f) => ({ ...f, t: retime(f.t, cam.lapses) })), out, shot.id);
      // The camera track's input: where each focus landed, in viewport pixels and
      // seconds from the clip's first frame. The compositor turns it into moves.
      writeFileSync(join(out, `${shot.id}.camera.json`), JSON.stringify({ viewport: { width, height }, events }, null, 2));
    }
    await ctx.close();
    return { width, height };
  } catch (e) {
    await page.screenshot({ path: join(out, `${shot.id}-failed.png`) }).catch(() => {});
    console.error(`marketing: shot ${shot.id}: ${current} failed: ${/** @type {Error} */ (e).message.split('\n')[0]}`);
    await ctx.close().catch(() => {});
    process.exit(1);
  }
}

/**
 * Takes the loop's frames: a screenshot at the context's 2x scale as often as the
 * page can give one, each stamped with when it was taken (the middle of the call),
 * in seconds from `origin`. Playwright's recordVideo cannot do this: its frames
 * are CSS pixels, so a video "at 2x" is the 1x frame padded with gray.
 * @param {import('@playwright/test').BrowserContext} ctx
 * @param {import('@playwright/test').Page} page
 * @param {number} origin Date.now() at the clip's start
 */
async function startFrames(ctx, page, origin) {
  const cdp = await ctx.newCDPSession(page);
  /** @type {{ t: number, data: string }[]} */
  const frames = [];
  let running = true;
  const taking = (async () => {
    while (running) {
      const a = Date.now();
      const r = await cdp.send('Page.captureScreenshot', { format: 'jpeg', quality: 92, optimizeForSpeed: true });
      frames.push({ t: ((a + Date.now()) / 2 - origin) / 1000, data: r.data });
    }
  })();
  return {
    async stop() {
      running = false;
      await taking;
      return frames;
    },
  };
}

/** @param {{ t: number, data: string }[]} frames @param {string} out @param {string} id */
function writeFrames(frames, out, id) {
  const dir = `${id}.frames`;
  mkdirSync(join(out, dir), { recursive: true });
  const name = (/** @type {number} */ i) => `${dir}/${String(i).padStart(5, '0')}.jpg`;
  frames.forEach((f, i) => writeFileSync(join(out, name(i)), Buffer.from(f.data, 'base64')));
  writeFileSync(join(out, `${id}.frames.txt`), concatList(frames, name));
}

/** @typedef {{ events: import('./marketinglib.mjs').FocusEvent[], lapses: import('./marketinglib.mjs').Lapse[], now: () => number }} Camera */

/**
 * Logs a step's focus: the box it names (or null for the whole frame), stamped
 * with the clip's clock. `here` is the step's own target, already on screen.
 * @param {import('@playwright/test').Page} page
 * @param {import('./marketinglib.mjs').Step} step
 * @param {Camera} cam
 * @param {import('@playwright/test').Locator | null} own the step's own target, when it has one
 */
async function logFocus(page, step, cam, own) {
  if (step.focus === undefined) return;
  if (step.focus === 'out') return void cam.events.push({ t: cam.now(), box: null });
  const el = step.focus === 'here' ? own : locate(page, step.focus).filter({ visible: true }).first();
  if (!el) throw new Error('focus here needs a step with a target');
  // The console re-renders as a page settles (a blade closing, a route's table
  // arriving), so a box read the instant an element shows can find it gone; read
  // it again a moment later before calling the focus lost.
  let box = null;
  for (let i = 0; i < 12 && !box; i++) {
    if (i) await page.waitForTimeout(150);
    await el.waitFor({ state: 'visible', timeout: ACT_TIMEOUT });
    box = await el.boundingBox();
  }
  if (!box) throw new Error('the focus has no box on screen');
  cam.events.push({ t: cam.now(), box, ...(step.zoom ? { zoom: step.zoom } : {}) });
}

/**
 * One step, with the cursor gliding to a target before it acts. A focus is
 * logged as the step starts, so the camera arrives as the cursor does; on a
 * wait it is logged once the awaited element shows.
 * @param {import('@playwright/test').Page} page
 * @param {string} base
 * @param {import('./marketinglib.mjs').Step} step
 * @param {{ x: number, y: number }} cursor
 * @param {boolean} loop
 * @param {Camera} cam
 */
async function play(page, base, step, cursor, loop, cam) {
  switch (step.kind) {
    case 'goto':
      await page.goto(`${base}/web${step.path}`, { waitUntil: 'load', timeout: ACT_TIMEOUT });
      await page.waitForTimeout(PACE.load);
      await logFocus(page, step, cam, null);
      break;
    case 'press':
      await logFocus(page, step, cam, null);
      await page.keyboard.press(/** @type {string} */ (step.key));
      break;
    case 'wait': {
      const el = locate(page, step.target).filter({ visible: true }).first();
      const from = cam.now();
      await el.waitFor({ state: 'visible', timeout: step.timeout });
      if (step.fast) cam.lapses.push({ from, to: cam.now(), speed: step.fast });
      await logFocus(page, step, cam, el);
      break;
    }
    default: {
      const el = locate(page, step.target);
      await el.waitFor({ state: 'visible', timeout: ACT_TIMEOUT });
      await el.scrollIntoViewIfNeeded({ timeout: ACT_TIMEOUT });
      await logFocus(page, step, cam, el);
      const box = await el.boundingBox();
      if (!box) throw new Error('the target has no box on screen');
      const x = step.kind === 'click' || step.kind === 'hover' ? box.x + box.width / 2 : Math.min(box.x + 24, box.x + box.width / 2);
      const y = box.y + box.height / 2;
      await page.mouse.move(x, y, { steps: 6 });
      Object.assign(cursor, { x, y });
      if (loop) await page.waitForTimeout(PACE.glide);
      if (step.kind === 'click') await el.click({ timeout: ACT_TIMEOUT });
      else if (step.kind === 'type') {
        await el.click({ timeout: ACT_TIMEOUT });
        if (step.replace) await el.fill('');
        await el.pressSequentially(/** @type {string} */ (step.text), { delay: PACE.key });
      } else if (step.kind === 'select') await el.selectOption(/** @type {string} */ (step.value), { timeout: ACT_TIMEOUT });
      // a hover is the move itself
    }
  }
  if (step.hold) await page.waitForTimeout(step.hold);
}

// Dispatch last: the definitions above (CURSOR, STYLE, consts) must exist first.
await main();
