// @ts-check
// The pure core of the marketing captures (#2299): the shot list
// (web/e2e/marketing/shots.yaml) parsed and validated, a loop's budget, the
// manifest the landing page reads (shots.json), the destination check, and the
// capture-only stylesheet. No I/O and no clock, so all of it is unit tested
// (web/src/marketing-shots.test.ts); web/e2e/marketing.mjs is the thin recorder
// around it and web/e2e/marketing.sh the lab and the encoders.
//
// A shot is `still` (one 2x WebP of a console state) or `loop` (an H.264 MP4 of
// a short interaction, with a WebP poster). Its steps use the walkthrough
// grammar (web/e2e/walkthroughlib.mjs: goto, click, hover, type, select, press,
// wait) without captions: a marketing capture speaks for itself, and the page
// that hosts it writes the words.

import { load } from 'js-yaml';

/** @typedef {{ width: number, height: number }} Viewport */
/** @typedef {{ method: 'PUT' | 'POST' | 'PATCH', path: string, body: Record<string, unknown> }} Call */
/** @typedef {{ get: string, field: string, equals: unknown, timeout: number }} Await */
/** @typedef {({ kind: 'call' } & Call) | ({ kind: 'await' } & Await)} SetupItem */
/** @typedef {string | { role: string, name?: string, exact?: boolean } | { text: string, exact?: boolean } | { selector: string }} Target */
/** @typedef {'out' | 'here' | Target} Focus */
/** @typedef {{ n: number, kind: string, path?: string, key?: string, target?: Target, text?: string, value?: string, replace?: boolean, timeout: number, hold: number, focus?: Focus, zoom?: number, fast?: number }} Step */
/** @typedef {{ id: string, kind: 'still' | 'loop', alt: string, viewport: Viewport, start: string, ready: Target | null, setup: SetupItem[], prep: Step[], steps: Step[], maxSeconds: number }} Shot */
/** @typedef {{ lab: { scenario: string, systems: number, omnibotReplay: string }, setup: SetupItem[], shots: Shot[] }} ShotList */

// A loop plays long enough to read and short enough to stay lively: 5 to 9
// seconds, and small enough to ship on a page: 1.5 MB.
export const LOOP_MIN_SECONDS = 5;
export const LOOP_MAX_SECONDS = 9;
// A loop may raise its own maximum up to this, for a story that cannot be told
// faster without lying about it: omnibot's answer takes as long as inference does.
export const LOOP_MAX_SECONDS_CAP = 15;
export const LOOP_MAX_BYTES = 1_500_000;
export const DEFAULT_VIEWPORT = { width: 1440, height: 900 };
// Longest a wait or an await may be given, in milliseconds.
export const MAX_WAIT_MS = 180_000;
export const DEFAULT_WAIT_MS = 15_000;
const MAX_HOLD_MS = 8000;

export const STEP_KINDS = ['goto', 'click', 'hover', 'type', 'select', 'press', 'wait'];
const STEP_KEYS = [...STEP_KINDS, 'hold', 'focus', 'zoom', 'fast'];

// The camera (a loop only): a step's `focus` eases the frame onto a box, so a
// clip draws the eye to what changed. `here` is the step's own target, `out`
// is the whole frame, and anything else is a target of its own. `zoom` sets the
// scale instead of fitting the box. Moves ease over CAMERA.ease seconds and
// never show past the capture's edge; a clip that ends zoomed eases back out
// before it loops, so the seam is invisible.
export const CAMERA = { ease: 0.6, maxZoom: 2, minZoom: 1, margin: 0.1 };
export const MAX_FAST = 8;
const ID = /^[a-z0-9][a-z0-9-]*$/;
const isObj = (/** @type {unknown} */ v) => typeof v === 'object' && v !== null && !Array.isArray(v);
const isStr = (/** @type {unknown} */ v) => typeof v === 'string' && v.length > 0;

// The capture-only stylesheet: it removes placeholders, never data. The lab
// banner says the instance is a lab, and a `soon` nav item is a destination that
// has not landed; neither belongs in a picture of the product. Everything else
// the console renders stays, the figures and the states included.
export const CAPTURE_CSS = [
  '[data-testid="lab-banner"] { display: none !important; }',
  'li:has(> a > [data-soon]) { display: none !important; }',
].join('\n');

/**
 * A console address in a request path: every {braced} part is percent-encoded
 * whole, so a component's path address (dots and a `$comp` segment) names the
 * one route segment it is. `/components/{a.$comp.b}/impairment` becomes
 * `/components/a.%24comp.b/impairment`.
 * @param {string} path
 * @returns {string}
 */
export function expandAddress(path) {
  return path.replace(/\{([^}]*)\}/g, (_, addr) => encodeURIComponent(addr));
}

/**
 * The value at a dotted path (`alarms.0.active`) of a parsed body, or undefined.
 * @param {unknown} obj
 * @param {string} field
 * @returns {unknown}
 */
export function pick(obj, field) {
  /** @type {any} */
  let cur = obj;
  for (const part of field.split('.')) {
    if (cur === null || cur === undefined) return undefined;
    cur = Array.isArray(cur) ? cur[Number(part)] : cur[part];
  }
  return cur;
}

/** @param {unknown} want @param {unknown} got */
export const conditionMet = (want, got) => JSON.stringify(want) === JSON.stringify(got);

/**
 * @param {unknown} t
 * @param {string} where
 * @param {boolean} allowText
 * @returns {string[]}
 */
function targetProblems(t, where, allowText) {
  if (isStr(t)) return [];
  if (!isObj(t)) return [`${where} must be a selector string or an object`];
  const o = /** @type {Record<string, unknown>} */ (t);
  if ('role' in o) {
    const bad = Object.keys(o).filter((k) => !['role', 'name', 'exact'].includes(k));
    const out = bad.map((k) => `${where} has unknown key "${k}"`);
    if (!isStr(o.role)) out.push(`${where}.role must be a non-empty string`);
    return out;
  }
  if (allowText && 'text' in o) return isStr(o.text) ? [] : [`${where}.text must be a non-empty string`];
  if (allowText && 'selector' in o) return isStr(o.selector) ? [] : [`${where}.selector must be a non-empty string`];
  return [`${where} needs ${allowText ? 'text, selector, or role' : 'a selector string or role'}`];
}

/**
 * @param {unknown} raw
 * @param {number} n
 * @param {string} [label] what the step list is called in a message (step, prep step)
 * @returns {[Step | null, string[]]}
 */
function parseStep(raw, n, label = 'step') {
  const at = `${label} ${n}`;
  if (!isObj(raw)) return [null, [`${at} must be a mapping`]];
  const o = /** @type {Record<string, any>} */ (raw);
  const kinds = Object.keys(o).filter((k) => STEP_KINDS.includes(k));
  const unknown = Object.keys(o).filter((k) => !STEP_KEYS.includes(k));
  if (unknown.includes('caption')) return [null, [`${at} has a caption: a marketing capture has none`]];
  if (kinds.length !== 1) return [null, [`${at} must have exactly one kind of ${STEP_KINDS.join(', ')}`]];
  const kind = kinds[0];
  const where = `${at} (${kind})`;
  const problems = unknown.map((k) => `${where} has unknown key "${k}"`);
  if ('hold' in o && !(Number.isInteger(o.hold) && o.hold >= 0 && o.hold <= MAX_HOLD_MS)) {
    problems.push(`${where}: hold must be whole milliseconds, 0 to ${MAX_HOLD_MS}`);
  }
  /** @type {Step} */
  const step = { n, kind, timeout: DEFAULT_WAIT_MS, hold: o.hold ?? 0 };
  if ('focus' in o) {
    const f = o.focus;
    if (f === 'here') {
      if (!['click', 'hover', 'type', 'select', 'wait'].includes(kind)) problems.push(`${where}: focus here needs a step with a target`);
    } else if (f !== 'out') {
      problems.push(...targetProblems(f, `${where} focus`, true));
    }
    step.focus = f;
  }
  if ('zoom' in o) {
    if (!(typeof o.zoom === 'number' && o.zoom >= CAMERA.minZoom && o.zoom <= CAMERA.maxZoom)) {
      problems.push(`${where}: zoom must be a number from ${CAMERA.minZoom} to ${CAMERA.maxZoom}`);
    }
    if (!('focus' in o) || o.focus === 'out') problems.push(`${where}: zoom goes with a focus on a target`);
    step.zoom = o.zoom;
  }
  // fast: N plays a wait N times faster, the quiet seconds before something
  // changes (a loop has 9 to spend, and a console that needs 5 to recover is not
  // one to sit through). The clip is a time-lapse there, and says so in the docs.
  if ('fast' in o) {
    if (kind !== 'wait') problems.push(`${where}: fast belongs on a wait`);
    else if (!(typeof o.fast === 'number' && o.fast > 1 && o.fast <= MAX_FAST)) problems.push(`${where}: fast is a speed-up over 1 and at most ${MAX_FAST}`);
    else step.fast = o.fast;
  }
  const v = o[kind];
  switch (kind) {
    case 'goto':
      if (!isStr(v) || !v.startsWith('/')) problems.push(`${where} must be a console path starting with "/"`);
      step.path = v;
      break;
    case 'click':
    case 'hover':
      problems.push(...targetProblems(v, where, false));
      step.target = v;
      break;
    case 'press':
      if (!isStr(v)) problems.push(`${where} must name a key`);
      step.key = v;
      break;
    case 'type':
    case 'select': {
      const field = kind === 'type' ? 'text' : 'value';
      if (!isObj(v)) {
        problems.push(`${where} must be { target, ${field} }`);
        break;
      }
      const known = kind === 'type' ? ['target', field, 'replace'] : ['target', field];
      for (const k of Object.keys(v)) if (!known.includes(k)) problems.push(`${where} has unknown key "${k}"`);
      problems.push(...targetProblems(v.target, `${where} target`, false));
      if (typeof v[field] !== 'string') problems.push(`${where} needs ${field}`);
      // replace: true clears the field first, so the typed text is the whole value.
      if ('replace' in v && typeof v.replace !== 'boolean') problems.push(`${where}: replace is true or false`);
      step.target = v.target;
      step[field] = v[field];
      if (v.replace === true) step.replace = true;
      break;
    }
    case 'wait': {
      if (!isObj(v)) {
        problems.push(`${where} must be { text | selector | role, timeout? }`);
        break;
      }
      const { timeout, ...target } = v;
      if (timeout !== undefined && !(Number.isInteger(timeout) && timeout > 0 && timeout <= MAX_WAIT_MS)) {
        problems.push(`${where}: timeout must be whole milliseconds, 1 to ${MAX_WAIT_MS}`);
      }
      problems.push(...targetProblems(target, where, true));
      step.target = /** @type {Target} */ (target);
      step.timeout = timeout ?? DEFAULT_WAIT_MS;
      break;
    }
  }
  return [step, problems];
}

/**
 * Setup items: a request (`put`, `post` or `patch` with a path and a body) or an
 * `await`ed state (`wait: { get, field, equals, timeout }`), run in order off camera.
 * @param {unknown} raw
 * @param {string} where
 * @returns {[SetupItem[], string[]]}
 */
function parseSetup(raw, where) {
  if (raw === undefined) return [[], []];
  if (!Array.isArray(raw)) return [[], [`${where} setup must be a list`]];
  /** @type {SetupItem[]} */
  const items = [];
  /** @type {string[]} */
  const problems = [];
  raw.forEach((it, i) => {
    const at = `${where} setup item ${i + 1}`;
    if (!isObj(it)) return void problems.push(`${at} must be a mapping`);
    const o = /** @type {Record<string, any>} */ (it);
    const methods = ['put', 'post', 'patch'].filter((m) => m in o);
    if (methods.length === 1) {
      const m = methods[0];
      for (const k of Object.keys(o)) if (k !== m && k !== 'body') problems.push(`${at} has unknown key "${k}"`);
      if (!isStr(o[m]) || !o[m].startsWith('/')) problems.push(`${at}: ${m} must be an API path starting with "/"`);
      if (!isObj(o.body)) problems.push(`${at}: needs a body mapping`);
      items.push({ kind: 'call', method: /** @type {any} */ (m.toUpperCase()), path: o[m], body: o.body ?? {} });
    } else if ('wait' in o) {
      const w = o.wait;
      if (!isObj(w)) return void problems.push(`${at}: wait must be { get, field, equals, timeout? }`);
      for (const k of Object.keys(w)) if (!['get', 'field', 'equals', 'timeout'].includes(k)) problems.push(`${at} has unknown key "${k}"`);
      if (!isStr(w.get) || !w.get.startsWith('/')) problems.push(`${at}: wait.get must be an API path starting with "/"`);
      if (!isStr(w.field)) problems.push(`${at}: wait.field must be a dotted field name`);
      if (!('equals' in w)) problems.push(`${at}: wait.equals is the value waited for`);
      if (w.timeout !== undefined && !(Number.isInteger(w.timeout) && w.timeout > 0 && w.timeout <= MAX_WAIT_MS)) {
        problems.push(`${at}: wait.timeout must be whole milliseconds, 1 to ${MAX_WAIT_MS}`);
      }
      items.push({ kind: 'await', get: w.get, field: w.field, equals: w.equals, timeout: w.timeout ?? 90_000 });
    } else {
      problems.push(`${at} must be one of put, post, patch or wait`);
    }
  });
  return [items, problems];
}

/** @param {unknown} v @param {string} where @returns {[Viewport | null, string[]]} */
function parseViewport(v, where) {
  if (v === undefined) return [null, []];
  if (!isObj(v)) return [null, [`${where} viewport must be { width, height }`]];
  const o = /** @type {Record<string, any>} */ (v);
  const ok = (/** @type {unknown} */ n) => Number.isInteger(n) && /** @type {number} */ (n) >= 320 && /** @type {number} */ (n) <= 3840;
  if (!ok(o.width) || !ok(o.height)) return [null, [`${where} viewport width and height must be whole pixels, 320 to 3840`]];
  return [{ width: o.width, height: o.height }, []];
}

/**
 * Parses and validates the shot list. Every problem is returned together, each
 * naming the shot and the step, so a bad list is fixed in one pass.
 * @param {string} text
 * @returns {{ list: ShotList | null, problems: string[] }}
 */
export function parseShots(text) {
  /** @type {unknown} */
  let doc;
  try {
    doc = load(text);
  } catch (e) {
    return { list: null, problems: [`the shot list is not YAML: ${/** @type {Error} */ (e).message}`] };
  }
  if (!isObj(doc)) return { list: null, problems: ['the shot list must be a mapping with lab and shots'] };
  const d = /** @type {Record<string, any>} */ (doc);
  /** @type {string[]} */
  const problems = [];
  for (const k of Object.keys(d)) if (!['lab', 'viewport', 'setup', 'shots'].includes(k)) problems.push(`unknown key "${k}"`);

  const lab = d.lab;
  if (!isObj(lab) || !isStr(lab.scenario) || !Number.isInteger(lab.systems) || lab.systems < 1) {
    problems.push('lab must be { scenario, systems (a positive whole number), omnibot_replay? }');
  }
  const [defaultViewport, vp] = parseViewport(d.viewport, 'the list');
  problems.push(...vp);
  const [setup, sp] = parseSetup(d.setup, 'the list');
  problems.push(...sp);

  /** @type {Shot[]} */
  const shots = [];
  if (!Array.isArray(d.shots) || d.shots.length === 0) {
    problems.push('shots must be a non-empty list');
  } else {
    const seen = new Set();
    d.shots.forEach((/** @type {any} */ s, /** @type {number} */ i) => {
      const at = isObj(s) && isStr(s.id) ? `shot ${s.id}` : `shot ${i + 1}`;
      if (!isObj(s)) return void problems.push(`${at} must be a mapping`);
      for (const k of Object.keys(s)) {
        if (!['id', 'kind', 'alt', 'viewport', 'start', 'ready', 'setup', 'prep', 'steps', 'max_seconds'].includes(k)) problems.push(`${at} has unknown key "${k}"`);
      }
      if (!isStr(s.id) || !ID.test(s.id)) problems.push(`${at}: id must be lowercase letters, digits and hyphens (it names its files)`);
      else if (seen.has(s.id)) problems.push(`${at}: id is used twice`);
      else seen.add(s.id);
      if (s.kind !== 'still' && s.kind !== 'loop') problems.push(`${at}: kind is still or loop`);
      if (!isStr(s.alt)) problems.push(`${at}: needs alt text (the page's description of the capture)`);
      if (!isStr(s.start) || !s.start.startsWith('/')) problems.push(`${at}: start must be a console path starting with "/"`);
      const [vpShot, vps] = parseViewport(s.viewport, at);
      problems.push(...vps);
      const [ssetup, ssp] = parseSetup(s.setup, at);
      problems.push(...ssp);
      /** @type {Target | null} */
      let ready = null;
      if (s.ready !== undefined) {
        problems.push(...targetProblems(s.ready, `${at}: ready`, true));
        ready = s.ready;
      }
      // Prep steps set the stage before the recording starts and are cut from it:
      // a loop that begins on the problem, not on the clicks that reach it.
      /** @type {Step[]} */
      const prep = [];
      if (s.prep !== undefined && !Array.isArray(s.prep)) problems.push(`${at}: prep must be a list`);
      else (s.prep ?? []).forEach((/** @type {unknown} */ st, /** @type {number} */ j) => {
        const [step, p] = parseStep(st, j + 1, 'prep step');
        problems.push(...p.map((m) => `${at}: ${m}`));
        if (step) prep.push(step);
      });
      if (s.prep !== undefined && s.kind !== 'loop') problems.push(`${at}: prep belongs on a loop (a still is shot after its steps)`);
      /** @type {Step[]} */
      const steps = [];
      if (s.steps !== undefined && !Array.isArray(s.steps)) problems.push(`${at}: steps must be a list`);
      else (s.steps ?? []).forEach((/** @type {unknown} */ st, /** @type {number} */ j) => {
        const [step, p] = parseStep(st, j + 1);
        problems.push(...p.map((m) => `${at}: ${m}`));
        if (step) steps.push(step);
      });
      if (s.kind === 'loop' && steps.length === 0) problems.push(`${at}: a loop needs steps`);
      for (const st of [...prep, ...steps]) {
        if (st.focus !== undefined && s.kind !== 'loop') problems.push(`${at}: step ${st.n} has a focus: the camera moves on a loop only`);
      }
      for (const st of prep) {
        if (st.focus !== undefined && s.kind === 'loop') problems.push(`${at}: prep step ${st.n} has a focus: prep is cut from the recording`);
      }
      let maxSeconds = LOOP_MAX_SECONDS;
      if (s.max_seconds !== undefined) {
        if (s.kind !== 'loop') problems.push(`${at}: max_seconds belongs on a loop`);
        else if (!(typeof s.max_seconds === 'number' && s.max_seconds > LOOP_MAX_SECONDS && s.max_seconds <= LOOP_MAX_SECONDS_CAP)) problems.push(`${at}: max_seconds is over ${LOOP_MAX_SECONDS} and at most ${LOOP_MAX_SECONDS_CAP}`);
        else maxSeconds = s.max_seconds;
      }
      shots.push({
        id: s.id, kind: s.kind, alt: s.alt, viewport: vpShot ?? defaultViewport ?? DEFAULT_VIEWPORT,
        start: s.start, ready, setup: ssetup, prep, steps, maxSeconds,
      });
    });
  }
  if (problems.length > 0) return { list: null, problems };
  return {
    list: {
      lab: { scenario: lab.scenario, systems: lab.systems, omnibotReplay: lab.omnibot_replay ?? '' },
      setup,
      shots,
    },
    problems: [],
  };
}

/**
 * A loop's budget: its length in seconds and its size in bytes. Returns the
 * problems, each naming the shot and the limit; none means within budget.
 * @param {string} id
 * @param {{ seconds: number, bytes: number }} m
 * @returns {string[]}
 */
export function budgetProblems(id, { seconds, bytes }, max = LOOP_MAX_SECONDS) {
  /** @type {string[]} */
  const out = [];
  if (!(seconds >= LOOP_MIN_SECONDS)) out.push(`shot ${id}: loop is ${seconds.toFixed(1)}s, under the ${LOOP_MIN_SECONDS}s minimum`);
  if (seconds > max) out.push(`shot ${id}: loop is ${seconds.toFixed(1)}s, over the ${max}s maximum`);
  if (bytes > LOOP_MAX_BYTES) out.push(`shot ${id}: loop is ${bytes} bytes, over the ${LOOP_MAX_BYTES}-byte limit`);
  return out;
}

/**
 * The destination the captures are written to: set, absolute, and not inside the
 * repo it records from is the caller's concern; this is the shape check. The
 * shell adds that the directory exists.
 * @param {string | undefined} dest
 * @returns {string | null} the problem, or null when it is usable
 */
export function destProblem(dest) {
  if (dest === undefined || dest === '') return 'MARKETING_DEST is not set: name the directory the captures are written to';
  if (!dest.startsWith('/')) return `MARKETING_DEST "${dest}" is not an absolute path`;
  return null;
}

const BARE = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;

/**
 * The manifest the landing page reads (shots.json): when, which lab, and each
 * shot's files (bare names beside the manifest), alt text and pixel size.
 * Throws on a shot the page could not use: a missing alt, a file with a path.
 * @param {{ generatedAt: string, lab: { scenario: string, systems: number }, shots: { id: string, kind: 'still' | 'loop', file: string, poster?: string, alt: string, width: number, height: number }[] }} m
 */
export function buildManifest({ generatedAt, lab, shots }) {
  /** @type {Record<string, unknown>[]} */
  const out = [];
  for (const s of shots) {
    if (!isStr(s.alt)) throw new Error(`shot ${s.id}: alt is required`);
    for (const f of [s.file, ...(s.kind === 'loop' ? [s.poster] : [])]) {
      if (typeof f !== 'string' || !BARE.test(f)) throw new Error(`shot ${s.id}: "${f}" is not a bare file name`);
    }
    if (s.kind === 'loop' && !s.poster) throw new Error(`shot ${s.id}: a loop needs a poster`);
    out.push({
      id: s.id, kind: s.kind, file: s.file,
      ...(s.kind === 'loop' ? { poster: s.poster } : {}),
      alt: s.alt, width: s.width, height: s.height,
    });
  }
  return { generatedAt, lab: { scenario: lab.scenario, systems: lab.systems }, shots: out };
}

/**
 * The files a shot writes, by kind: a still is one WebP, a loop an MP4 with a
 * WebP poster (named for the still's frame; a still has no separate poster).
 * @param {{ id: string, kind: 'still' | 'loop' }} s
 * @returns {{ file: string, poster: string }}
 */
export function shotFiles(s) {
  return { file: s.kind === 'loop' ? `${s.id}.mp4` : `${s.id}.webp`, poster: `${s.id}.webp` };
}

const RESOURCE = /\/(components|systems|endpoints)\/(\{[^}]*\}|[^/?]+)/g;
const KIND = { components: 'component', systems: 'system', endpoints: 'endpoint' };

/**
 * The resources a list names that must already exist on the lab before anything
 * is captured: the component, system or endpoint a setup item's path (or a
 * `start`) addresses. A resource a setup item creates (a secret) is not one.
 * @param {SetupItem[]} items
 * @param {string | null} start
 * @returns {{ kind: string, id: string, path: string }[]}
 */
export function referencedResources(items, start) {
  /** @type {Map<string, { kind: string, id: string, path: string }>} */
  const found = new Map();
  const paths = [...items.map((it) => (it.kind === 'call' ? it.path : it.get)), ...(start ? [start] : [])];
  for (const p of paths) {
    for (const m of p.matchAll(RESOURCE)) {
      const id = m[2].startsWith('{') ? m[2].slice(1, -1) : m[2];
      found.set(`${m[1]}/${id}`, { kind: KIND[/** @type {'components'} */ (m[1])], id, path: `/${m[1]}/${m[2]}` });
    }
  }
  return [...found.values()];
}

/** @typedef {{ x: number, y: number, width: number, height: number }} Box */
/** @typedef {{ t: number, box: Box | null, zoom?: number }} FocusEvent */
/** @typedef {{ at: number, dur: number, scale: number, x: number, y: number }} CameraMove */

/**
 * The frame a focus asks for: the scale that fits the box with a margin (or the
 * step's zoom), capped, and the centre moved in so the frame never shows past
 * the capture's edge. A null box is the whole frame.
 * @param {Box | null} box in CSS pixels of the viewport
 * @param {Viewport} vp
 * @param {number} [zoom]
 * @returns {{ scale: number, x: number, y: number }}
 */
export function frameFor(box, vp, zoom) {
  const full = { scale: 1, x: vp.width / 2, y: vp.height / 2 };
  if (!box) return full;
  const fit = Math.min(
    (vp.width * (1 - 2 * CAMERA.margin)) / Math.max(box.width, 1),
    (vp.height * (1 - 2 * CAMERA.margin)) / Math.max(box.height, 1),
  );
  const scale = Math.min(CAMERA.maxZoom, Math.max(CAMERA.minZoom, zoom ?? fit));
  const halfW = vp.width / (2 * scale), halfH = vp.height / (2 * scale);
  const clamp = (/** @type {number} */ v, /** @type {number} */ lo, /** @type {number} */ hi) => Math.min(Math.max(v, lo), hi);
  return {
    scale: round(scale),
    x: round(clamp(box.x + box.width / 2, halfW, vp.width - halfW)),
    y: round(clamp(box.y + box.height / 2, halfH, vp.height - halfH)),
  };
}

const round = (/** @type {number} */ n) => Math.round(n * 1000) / 1000;

/**
 * A loop's camera: the focus events the recorder logged (seconds from the
 * clip's first frame, a box or null for the whole frame) turned into moves the
 * compositor plays. Moves run one after another: one that would start before
 * the last has landed waits for it. A move that changes nothing is dropped. A
 * clip left zoomed eases back to the whole frame so it ends where it began.
 * @param {FocusEvent[]} events
 * @param {Viewport} vp
 * @param {number} duration the clip's length in seconds
 * @returns {CameraMove[]}
 */
export function cameraTrack(events, vp, duration) {
  /** @type {CameraMove[]} */
  const moves = [];
  let cur = frameFor(null, vp);
  let free = 0;
  for (const e of [...events].sort((a, b) => a.t - b.t)) {
    const f = frameFor(e.box, vp, e.zoom);
    if (f.scale === cur.scale && f.x === cur.x && f.y === cur.y) continue;
    const at = round(Math.max(0, e.t, free));
    if (at + CAMERA.ease > duration) break;
    moves.push({ at, dur: CAMERA.ease, ...f });
    cur = f;
    free = at + CAMERA.ease;
  }
  if (cur.scale !== 1) {
    const at = round(Math.max(free, duration - CAMERA.ease));
    const dur = round(Math.min(CAMERA.ease, duration - at));
    if (dur > 0) moves.push({ at, dur, ...frameFor(null, vp) });
  }
  return moves;
}

// The frames of a loop are screenshots at twice the viewport's pixels, taken as
// fast as the page allows (Playwright's own video is CSS-pixel sized, so a 2x
// recording of it is the 1x frame padded with gray). Each frame shows from its
// own time to the next one's; the first shows from the clip's start.
export const FRAME_FALLBACK_SECONDS = 0.04;

/**
 * The ffmpeg concat-demuxer list for a loop's frames, so a variable frame rate
 * plays at the pace it was taken.
 * @param {{ t: number }[]} frames times in seconds from the clip's start, ascending
 * @param {(i: number) => string} name the file name of frame i, relative to the list
 * @returns {string}
 */
export function concatList(frames, name) {
  if (frames.length === 0) throw new Error('a loop with no frames cannot be encoded');
  const lines = [];
  frames.forEach((f, i) => {
    const from = i === 0 ? 0 : f.t;
    const until = i + 1 < frames.length ? frames[i + 1].t : f.t + FRAME_FALLBACK_SECONDS;
    lines.push(`file '${name(i)}'`, `duration ${Math.max(0.001, until - from).toFixed(4)}`);
  });
  // The demuxer ignores the last duration unless its file is named again.
  lines.push(`file '${name(frames.length - 1)}'`);
  return lines.join('\n') + '\n';
}

/** @typedef {{ from: number, to: number, speed: number }} Lapse */

/**
 * Where a real-time moment lands in a clip whose `lapses` (the waits played
 * faster) are shortened: each second inside a lapse counts for 1/speed.
 * @param {number} t seconds in real time from the clip's start
 * @param {Lapse[]} lapses ascending and not overlapping
 * @returns {number}
 */
export function retime(t, lapses) {
  let out = t;
  for (const w of lapses) {
    const inside = Math.min(Math.max(t, w.from), w.to) - w.from;
    out -= inside * (1 - 1 / w.speed);
  }
  return out;
}

/**
 * How long to keep recording once the last step is done, so the camera has the
 * time to ease back to the whole frame: a clip that ends on a focus would
 * otherwise loop mid-zoom.
 * @param {FocusEvent[]} events
 * @returns {number} seconds
 */
export function cameraTail(events) {
  const last = [...events].sort((a, b) => a.t - b.t).at(-1);
  return last && last.box ? CAMERA.ease + 0.15 : 0;
}
