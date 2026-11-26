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
//   node e2e/marketing.mjs budget <id> <seconds> <bytes> [max]   a loop's budget, exit 1 over
//   node e2e/marketing.mjs max-bytes [max]             the byte budget of a loop allowed max seconds
//   node e2e/marketing.mjs ids <shots.yaml>            "<id> <kind> <max seconds>" per shot, in order
//   node e2e/marketing.mjs manifest <shots.yaml> <dest> <dims.json>   write shots.json
//
// A failing step fails the run naming the shot, the step, its kind and what it
// waited for, and keeps the frame it failed on beside the captures.

import { appendFileSync, existsSync, readFileSync, rmSync, statSync, writeFileSync, mkdirSync } from 'node:fs';
import { load } from 'js-yaml';
import { dirname, join } from 'node:path';
import { CAPTURE_CSS, LOOP_MAX_SECONDS, PACE, beatEnds, beatStill, budgetProblems, buildManifest, cameraTail, concatList, conditionMet, destProblem, expandAddress, frameFor, frameRect, lapseSpeed, loopMaxBytes, missingText, overrunReport, parseShots, pick, pointerX, referencedResources, replayProblems, retime, shotFiles, stepAuthoredSeconds } from './marketinglib.mjs';
import { describeTarget, locate } from './walkthroughlib.mjs';

const ACT_TIMEOUT = 15_000;
// How long the browser may refuse screenshots (a navigation on camera) before
// the frame loop opens its session again, then calls it a failure; and how long
// one capture may take before it counts as refused.
const FRAME_REFUSAL_MS = 10_000;
const FRAME_CAPTURE_MS = 1_000;
// How long a beat waits for the video to catch up with the page: a heavy
// render on a full lab stalls the screenshots for seconds after the page has
// changed, and a beat that held from the page's change played over old frames.
const FRESH_FRAME_MS = 8_000;
// Loops record at twice the viewport's pixels, so the camera's zoom stays sharp.
// Pacing (PACE, marketinglib.mjs) is shared with the parse, which holds a loop's
// own spend to its maximum.

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
    if (replay) {
      const rec = /** @type {any} */ (load(readFileSync(new URL(`../../internal/lab/omnibot/${replay}.yaml`, import.meta.url), 'utf8')));
      const prompts = [rec?.prompt, ...(rec?.conversations ?? []).map((/** @type {any} */ c) => c?.prompt)];
      const asked = replayProblems(list.shots, prompts);
      for (const p of asked) console.error(`marketing: ${file}: ${p}`);
      if (asked.length) process.exit(2);
    }
    console.log(`marketing: ${list.shots.length} shots, destination ${dest}`);
  } else if (mode === 'lab-args') {
    const l = readList(args[0]).lab;
    console.log(`${l.scenario} ${l.systems} ${l.omnibotReplay}`);
  } else if (mode === 'ids') {
    for (const s of readList(args[0]).shots) console.log(s.id, s.kind, s.maxSeconds);
  } else if (mode === 'budget') {
    const [id, seconds, bytes, max, stepsFile] = args;
    const limit = max ? Number(max) : LOOP_MAX_SECONDS;
    const problems = budgetProblems(id, { seconds: Number(seconds), bytes: Number(bytes) }, limit);
    // A loop past its maximum says where its seconds went, whether or not it is
    // inside the allowance for the lab's speed: the next fix starts there.
    if (stepsFile && existsSync(stepsFile)) {
      for (const line of overrunReport(id, Number(seconds), limit, JSON.parse(readFileSync(stepsFile, 'utf8')).steps)) console.error(`marketing: ${line}`);
    }
    for (const p of problems) console.error(`marketing: ${p}`);
    process.exit(problems.length ? 1 : 0);
  } else if (mode === 'max-bytes') {
    console.log(loopMaxBytes(args[0] ? Number(args[0]) : LOOP_MAX_SECONDS));
  } else if (mode === 't0') {
    console.log(JSON.parse(readFileSync(args[0], 'utf8'))[args[1]].t0);
  } else if (mode === 'beat-times') {
    // One line per beat of a loop's beats.json: its number and the second of the
    // clip its storyboard still is taken at (the moment its frame was checked).
    JSON.parse(readFileSync(args[0], 'utf8')).beats.forEach((/** @type {import('./marketinglib.mjs').BeatTimes} */ b, /** @type {number} */ i) => console.log(i + 1, beatStill(b)));
  } else if (mode === 'manifest') {
    const [file, dest, dims] = args;
    const list = readList(file);
    // A loop's beats (when each opens and ends, and the part of the capture its
    // frame shows) ride along from the recording, when it wrote them.
    const beatsOf = (/** @type {string} */ id) => {
      const f = dims ? join(dirname(dims), `${id}.beats.json`) : '';
      if (!f || !existsSync(f)) return undefined;
      return JSON.parse(readFileSync(f, 'utf8')).beats.map((/** @type {{ step: number, from: number, to: number, frame: object }} */ b) => ({ step: b.step, from: b.from, to: b.to, frame: b.frame }));
    };
    const manifest = buildManifest({
      generatedAt: new Date().toISOString(),
      lab: { scenario: list.lab.scenario, systems: list.lab.systems },
      shots: list.shots.map((s) => ({
        id: s.id, kind: s.kind, ...shotFiles(s), alt: s.alt, width: s.viewport.width, height: s.viewport.height,
        ...(s.kind === 'loop' && beatsOf(s.id) ? { beats: beatsOf(s.id) } : {}),
      })),
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
  rmSync(join(out, 'frame-check.txt'), { force: true });
  const api = makeApi(base, token);

  // Before any capture: every component, system and endpoint a setup item or a
  // start names must exist on this lab, so a typo or a fleet that changed fails
  // here, naming the shot, rather than ten minutes into a recording.
  const bad = [];
  for (const [where, items, start] of [
    ['the list', list.setup, null],
    ...list.shots.map((s) => [
      `shot ${s.id}`,
      // A mid-clip call addresses the lab the same way a setup item does.
      [...s.setup, ...[...s.prep, ...s.steps].filter((st) => st.kind === 'call').map((st) => ({ kind: 'call', method: st.method, path: st.path, body: st.body }))],
      s.start,
    ]),
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
      dims[shot.id] = await record(browser, base, state, shot, out, api);
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
      if (it.kind === 'pause') {
        // The lab runs on off camera (#2722), so the state the shot opens on
        // has lasted this long.
        console.log(`marketing: ${where}: the lab runs on for ${it.seconds}s`);
        await new Promise((r) => setTimeout(r, it.seconds * 1000));
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
  // The click's ripple: a teal ring and a soft fill that spread from the
  // pointer, large enough to read in a clip played at a third of its size.
  ring.style.cssText = 'position:fixed;left:0;top:0;width:48px;height:48px;margin:-24px 0 0 -24px;border-radius:50%;border:3px solid #21cab9;background:rgba(33,202,185,.18);z-index:2147483646;pointer-events:none;opacity:0';
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
    ring.animate([{ opacity: 1, transform: 'scale(.3)' }, { opacity: 0, transform: 'scale(1.9)' }], { duration: 650, easing: 'cubic-bezier(.2,.7,.3,1)' });
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
 * @param {ReturnType<typeof makeApi>} api
 */
async function record(browser, base, storageState, shot, out, api) {
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
  const cam = { events: [], lapses: [], now: () => 0, vp: { width, height }, beats: [], open: null };
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
      await play(page, base, step, cursor, false, cam, api);
    }
    // The recording opens here, on the console's first paint: the clip's clock
    // (the camera's too) starts now.
    const t0 = Date.now();
    cam.now = () => (Date.now() - t0) / 1000;
    const frames = loop ? await startFrames(ctx, page, t0) : null;
    if (frames) cam.fresh = (since) => frames.fresh(since, FRESH_FRAME_MS);
    if (loop) await page.mouse.move(cursor.x, cursor.y);
    /** @type {{ n: number, kind: string, target: string, from: number, to: number, authored: number }[]} */
    const timed = [];
    for (const step of shot.steps) {
      current = `step ${step.n} (${step.kind}${step.target ? ` ${describeTarget(step.target)}` : ''})`;
      const from = cam.now();
      await play(page, base, step, cursor, loop, cam, api);
      timed.push({ n: step.n, kind: step.kind, target: step.target ? describeTarget(step.target) : '', from, to: cam.now(), authored: stepAuthoredSeconds(step) });
      if (process.env.MARKETING_TRACE) console.log(`  ${shot.id} ${current}: ${(cam.now() - from).toFixed(1)}s`);
    }
    if (loop) {
      current = 'last beat';
      await closeBeat(page, cam);
    }
    // Let the last state settle and the stills' data finish arriving.
    current = 'final frame';
    await page.waitForTimeout(shot.kind === 'still' ? 1500 : PACE.settle);
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
      // The lapses, in real time, mark the stretches played faster: the
      // compositor badges them so a sped-up wait never passes for real time.
      writeFileSync(join(out, `${shot.id}.camera.json`), JSON.stringify({ viewport: { width, height }, events, lapses: cam.lapses }, null, 2));
      // Each beat: when it opened, was checked and ended, the part of the capture
      // its frame shows, and the text on screen inside that frame once its step
      // had played. The storyboard takes one still per beat from the finished clip.
      const at = (/** @type {number} */ t) => round3(retime(t, cam.lapses));
      const beats = beatEnds(cam.beats, cam.events).map((b) => ({ ...b, from: at(b.from), at: at(b.at), to: at(b.to) }));
      writeFileSync(join(out, `${shot.id}.beats.json`), JSON.stringify({ viewport: { width, height }, beats }, null, 2));
      // Each step's seconds in the clip beside what the shot list spends on it,
      // so a loop over its maximum can say which steps the lab stretched.
      const steps = timed.map(({ from, to, ...t }) => ({ ...t, clip: round3(at(to) - at(from)), authored: round3(t.authored) }));
      writeFileSync(join(out, `${shot.id}.steps.json`), JSON.stringify({ steps }, null, 2));
      // A beat whose frame misses its expected text does not stop the run: every
      // shot is still recorded and encoded, with its storyboard, so one pass
      // shows them all. marketing.sh fails the run at its end, listing them.
      for (const b of beats.filter((x) => x.missing?.length)) {
        const line = `shot ${shot.id}: step ${b.step}'s frame does not show ${(b.missing ?? []).map((m) => JSON.stringify(m)).join(', ')}; it shows: ${b.seen.slice(0, 12).join(' | ') || 'no text'}`;
        console.error(`marketing: ${line}`);
        appendFileSync(join(out, 'frame-check.txt'), line + '\n');
      }
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
  let cdp = await ctx.newCDPSession(page);
  /** @type {{ t: number, data: string }[]} */
  const frames = [];
  // When the newest frame's screenshot was asked for: what it shows is the
  // page as it was then or later.
  let newest = 0;
  let running = true;
  const taking = (async () => {
    // A navigation on camera (a goto step, #2722) swaps the page out, and the
    // browser may refuse a screenshot, or not answer one, while it does. Each
    // capture is time-boxed, so none can hang the shot; a refused or stalled
    // one is skipped (the frame before it holds a little longer). Past
    // FRAME_REFUSAL_MS without a frame the session is opened again once, and a
    // second stretch fails the shot rather than recording nothing.
    let refusedSince = 0;
    let reopened = false;
    while (running) {
      const a = Date.now();
      /** @type {{ data: string } | Error} */
      const r = await Promise.race([
        cdp.send('Page.captureScreenshot', { format: 'jpeg', quality: 92, optimizeForSpeed: true }).catch((e) => /** @type {Error} */ (e)),
        new Promise((res) => setTimeout(() => res(new Error(`no screenshot within ${FRAME_CAPTURE_MS}ms`)), FRAME_CAPTURE_MS)),
      ]);
      if (!running) break;
      if (r instanceof Error) {
        refusedSince ||= a;
        console.error(`marketing: frame skipped: ${r.message.split('\n')[0]}`);
        if (Date.now() - refusedSince > FRAME_REFUSAL_MS) {
          if (reopened) throw r;
          reopened = true;
          refusedSince = 0;
          console.error('marketing: frames: opening the capture session again');
          await cdp.detach().catch(() => {});
          cdp = await ctx.newCDPSession(page);
          continue;
        }
        await new Promise((res) => setTimeout(res, 50));
        continue;
      }
      refusedSince = 0;
      newest = a;
      frames.push({ t: ((a + Date.now()) / 2 - origin) / 1000, data: r.data });
    }
  })();
  return {
    async stop() {
      running = false;
      await taking;
      return frames;
    },
    /**
     * Resolves true once a frame asked for at or after `since` (epoch ms) is
     * in, so the video shows the page as it was then; false past `timeout`.
     * @param {number} since @param {number} timeout
     */
    async fresh(since, timeout) {
      const end = Date.now() + timeout;
      while (newest < since) {
        if (!running || Date.now() > end) return false;
        await new Promise((res) => setTimeout(res, 25));
      }
      return true;
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

/** @typedef {{ step: number, from: number, at: number, to: number, frame: import('./marketinglib.mjs').Box, seen: string[], expect?: string[], missing?: string[] }} Beat */
/** @typedef {{ step: number, from: number, frame: import('./marketinglib.mjs').Box, onScreen: import('./marketinglib.mjs').Box, expect?: string[], at?: number, seen?: string[] }} OpenBeat */
/** @typedef {{ events: import('./marketinglib.mjs').FocusEvent[], lapses: import('./marketinglib.mjs').Lapse[], now: () => number, vp: { width: number, height: number }, beats: Beat[], open: OpenBeat | null, fresh?: (since: number) => Promise<boolean> }} Camera */

const round3 = (/** @type {number} */ n) => Math.round(n * 1000) / 1000;

/**
 * The text on screen inside a part of the page: each text node wholly inside
 * it, shown, and not covered by something else at its middle (a blade's dimmed
 * backdrop, a dialog), in document order, then each form control's shown value
 * (a select's chosen option, a text field's value) by the same tests.
 * @param {import('@playwright/test').Page} page
 * @param {import('./marketinglib.mjs').Box} r in viewport pixels
 * @returns {Promise<string[]>}
 */
function textInside(page, r) {
  return page.evaluate((r) => {
    /** @type {string[]} */
    const out = [];
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    for (let n = walker.nextNode(); n; n = walker.nextNode()) {
      const text = (n.textContent ?? '').replace(/\s+/g, ' ').trim();
      const el = n.parentElement;
      if (!text || !el || !el.checkVisibility({ opacityProperty: true, visibilityProperty: true })) continue;
      const range = document.createRange();
      range.selectNodeContents(n);
      const boxes = [...range.getClientRects()].filter((b) => b.width > 0 && b.height > 0);
      if (!boxes.length) continue;
      const inside = boxes.every((b) => b.left >= r.x - 1 && b.top >= r.y - 1 && b.right <= r.x + r.width + 1 && b.bottom <= r.y + r.height + 1);
      if (!inside) continue;
      // Hit-testing skips an element that takes no pointer events (a chart's
      // label drawn over its bar), so such text is judged by its nearest
      // ancestor that does take them: covered only when something outside it is on top.
      let probe = el;
      while (probe.parentElement && getComputedStyle(probe).pointerEvents === 'none') probe = probe.parentElement;
      const top = document.elementFromPoint(boxes[0].left + boxes[0].width / 2, boxes[0].top + boxes[0].height / 2);
      if (top && !probe.contains(top) && !top.contains(probe)) continue;
      out.push(text);
    }
    // A form control draws its own text: a select its chosen option, a text
    // field its value. Neither is a text node the walk above reaches, so each
    // is read from the control and boxed by the control itself.
    const typed = new Set(['', 'text', 'search', 'email', 'url', 'tel', 'number']);
    for (const c of document.querySelectorAll('select, input, textarea')) {
      const raw =
        c instanceof HTMLSelectElement ? (c.selectedOptions[0]?.text ?? '')
        : c instanceof HTMLInputElement ? (typed.has(c.type) ? c.value : '')
        : c instanceof HTMLTextAreaElement ? c.value
        : '';
      const text = raw.replace(/\s+/g, ' ').trim();
      if (!text || !c.checkVisibility({ opacityProperty: true, visibilityProperty: true })) continue;
      const b = c.getBoundingClientRect();
      if (b.width <= 0 || b.height <= 0) continue;
      if (b.left < r.x - 1 || b.top < r.y - 1 || b.right > r.x + r.width + 1 || b.bottom > r.y + r.height + 1) continue;
      const top = document.elementFromPoint(b.left + b.width / 2, b.top + b.height / 2);
      if (top && !c.contains(top) && !top.contains(c)) continue;
      out.push(text);
    }
    return out;
  }, r);
}

/**
 * Reads the text the open beat's frame shows now. It is read once, as the step
 * that opened the beat finishes (its hold played out): the moment the beat is
 * about. Later steps inside the beat (a click that opens a blade) change the page
 * on their way to the next beat, so a read at the beat's end would check that.
 * @param {import('@playwright/test').Page} page
 * @param {Camera} cam
 */
async function checkBeat(page, cam) {
  const b = cam.open;
  if (!b || b.seen) return;
  b.at = cam.now();
  b.seen = await textInside(page, b.onScreen);
}

/**
 * Ends the open beat, if any, and checks the text its frame showed against what
 * the beat expects.
 * @param {import('@playwright/test').Page} page
 * @param {Camera} cam
 */
async function closeBeat(page, cam) {
  await checkBeat(page, cam);
  const b = cam.open;
  if (!b) return;
  cam.open = null;
  const seen = b.seen ?? [];
  const missing = b.expect ? missingText(seen, b.expect) : [];
  cam.beats.push({ step: b.step, from: b.from, at: b.at ?? b.from, to: cam.now(), frame: b.frame, seen, ...(b.expect ? { expect: b.expect } : {}), ...(missing.length ? { missing } : {}) });
}

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
  // The beat opens once the video shows what the page now does, not before:
  // the camera lands on the target as it appears in the clip.
  if (cam.fresh && !(await cam.fresh(Date.now()))) {
    console.error(`marketing: step ${step.n}: the video had not caught up with the page after ${FRESH_FRAME_MS / 1000}s`);
  }
  const t = cam.now();
  cam.events.push({ t, box, ...(step.zoom ? { zoom: step.zoom } : {}), ...(step.spot === false ? { spot: false } : {}) });
  const { rect, onScreen } = frameRect(frameFor(box, cam.vp, step.zoom), cam.vp);
  cam.open = { step: step.n, from: t, frame: rect, onScreen, ...(step.expect ? { expect: step.expect } : {}) };
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
 * @param {ReturnType<typeof makeApi>} api
 */
async function play(page, base, step, cursor, loop, cam, api) {
  // A focus ends the beat before it: its frame is read as the camera leaves.
  if (loop && step.focus !== undefined) await closeBeat(page, cam);
  // A focus on a selector of its own is what the step reveals, so it is looked
  // for once the step has acted; `here` lands as the cursor does.
  const later = step.focus !== undefined && step.focus !== 'here' && step.focus !== 'out';
  switch (step.kind) {
    case 'goto':
      await page.goto(`${base}/web${step.path}`, { waitUntil: 'load', timeout: ACT_TIMEOUT });
      await page.waitForTimeout(PACE.load);
      await logFocus(page, step, cam, null);
      break;
    case 'call':
      // Off camera: the lab changes, and the console shows what it makes of it.
      await api.send(/** @type {string} */ (step.method), /** @type {string} */ (step.path), step.body);
      break;
    case 'press':
      if (!later) await logFocus(page, step, cam, null);
      await page.keyboard.press(/** @type {string} */ (step.key));
      if (later) await logFocus(page, step, cam, null);
      break;
    case 'wait': {
      if (step.seconds) {
        // A wait for time itself (#2722): the lab runs on, played as a lapse
        // when the step has one, so the record it leaves is there to read.
        const from = cam.now();
        await page.waitForTimeout(step.seconds * 1000);
        if (step.lapse) {
          const speed = lapseSpeed(cam.now() - from, step.lapse);
          if (speed > 1) cam.lapses.push({ from, to: cam.now(), speed });
        }
        await logFocus(page, step, cam, null);
        break;
      }
      const el = locate(page, step.target).filter({ visible: true }).first();
      const from = cam.now();
      await el.waitFor({ state: 'visible', timeout: step.timeout });
      // A lapse plays in the seconds the shot list gives it, however long this
      // run's wait took: the speed is the run's, the length is the shot's.
      if (step.lapse) {
        const speed = lapseSpeed(cam.now() - from, step.lapse);
        if (speed > 1) cam.lapses.push({ from, to: cam.now(), speed });
      }
      await logFocus(page, step, cam, el);
      break;
    }
    default: {
      const el = locate(page, step.target);
      await el.waitFor({ state: 'visible', timeout: ACT_TIMEOUT });
      await el.scrollIntoViewIfNeeded({ timeout: ACT_TIMEOUT });
      if (!later) await logFocus(page, step, cam, el);
      const box = await el.boundingBox();
      if (!box) throw new Error('the target has no box on screen');
      const x = pointerX(step.kind, box);
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
      if (later) await logFocus(page, step, cam, el);
    }
  }
  if (step.hold) await page.waitForTimeout(step.hold);
  // The step that opened a beat has played: check what its frame shows.
  if (loop && cam.open?.step === step.n) await checkBeat(page, cam);
}

// Dispatch last: the definitions above (CURSOR, STYLE, consts) must exist first.
await main();
