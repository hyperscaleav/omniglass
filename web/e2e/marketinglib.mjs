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
/** @typedef {({ kind: 'call' } & Call) | ({ kind: 'await' } & Await) | { kind: 'pause', seconds: number }} SetupItem */
/** @typedef {string | { role: string, name?: string, exact?: boolean } | { text: string, exact?: boolean } | { selector: string }} Target */
/** @typedef {'out' | 'here' | Target} Focus */
/** @typedef {{ n: number, kind: string, method?: 'PUT' | 'POST' | 'PATCH', body?: Record<string, unknown>, path?: string, key?: string, target?: Target, text?: string, value?: string, replace?: boolean, timeout: number, hold: number, focus?: Focus, zoom?: number, spot?: false, lapse?: number, seconds?: number, expect?: string[] }} Step */
/** @typedef {{ id: string, kind: 'still' | 'loop', alt: string, viewport: Viewport, start: string, ready: Target | null, setup: SetupItem[], prep: Step[], steps: Step[], maxSeconds: number }} Shot */
/** @typedef {{ lab: { scenario: string, systems: number, omnibotReplay: string, healthHistory: boolean }, setup: SetupItem[], shots: Shot[] }} ShotList */

// A loop plays long enough to read and short enough to stay lively: 5 to 9
// seconds, and small enough to ship on a page: 1.5 MB.
export const LOOP_MIN_SECONDS = 5;
export const LOOP_MAX_SECONDS = 9;
// A loop may raise its own maximum up to this, for a story that cannot be told
// faster without lying about it: omnibot's answer takes as long as inference
// does, and the page's teaser runs from the fleet to a room back to healthy.
export const LOOP_MAX_SECONDS_CAP = 60;
// How far past its maximum a recorded loop may run before the run fails. A
// loop's length is the lab's as much as the shot list's: each wait lasts as
// long as the console takes, and a scale lab on a busy host takes longer. The
// shot list's own spend is held to the maximum exactly, at parse time
// (authoredSeconds); the recording gets this much room for the lab's speed.
export const LOOP_TOLERANCE = 0.15;
// The byte budget of a loop at up to 15 seconds; a longer one earns the same
// rate for its length (loopMaxBytes), so the teaser may weigh twice the clip.
export const LOOP_MAX_BYTES = 1_500_000;
const LOOP_BUDGET_SECONDS = 15;
export const DEFAULT_VIEWPORT = { width: 1440, height: 900 };
// Longest a wait or an await may be given, in milliseconds.
export const MAX_WAIT_MS = 180_000;
// Longest a setup pause may hold the lab off camera, in seconds (#2722).
export const MAX_PAUSE_SECONDS = 900;
export const DEFAULT_WAIT_MS = 15_000;
const MAX_HOLD_MS = 8000;

export const STEP_KINDS = ['goto', 'click', 'hover', 'type', 'select', 'press', 'wait', 'call'];
const STEP_KEYS = [...STEP_KINDS, 'hold', 'focus', 'zoom', 'spot', 'fast', 'lapse', 'expect'];
// The longest a lapse may play: past this, a wait is the story, not a pause in it.
const MAX_LAPSE_SECONDS = 5;

// The camera (a loop only): a step's `focus` eases the frame onto a box, so a
// clip draws the eye to what changed. `here` is the step's own target, `out`
// is the whole frame, and anything else is a target of its own. `zoom` sets the
// scale instead of fitting the box. Moves ease over CAMERA.ease seconds and
// centre the box; a clip that ends zoomed eases back out before it loops, so
// the seam is invisible. A focus opens a beat, which runs until the next focus
// (or the clip's end); `expect` names text the frame must show as the beat ends.
// backdrop is the console's own base colour (--color-base-100, dark): the
// frame shows it past the capture's edge when a focus near that edge is
// centred.
// spotPad is the lit margin round a focus box, in viewport pixels; dim is how
// dark the rest of the frame goes while the camera is on a box.
export const CAMERA = { ease: 0.6, maxZoom: 3, minZoom: 1, margin: 0.1, backdrop: '#080c16', spotPad: 12, dim: 0.5, clickZoom: 2.2 };
// A wait played as a lapse runs at up to MAX_FAST times real speed: an alarm
// that takes a minute to raise plays in a second, and three minutes of a room
// running on (a timed wait, #2722) in under one, under a badge that says the
// clip is sped up and by how much (render.mjs), so no speed passes for real time.
export const MAX_FAST = 300;
// The recorder's own pace on a loop, in milliseconds: the pointer's glide to a
// target before it acts, each key typed, the settle after a page loads, and the
// moment the last state settles before the clip ends.
export const PACE = { glide: 250, key: 35, load: 350, settle: 300 };
const ID = /^[a-z0-9][a-z0-9-]*$/;
const isObj = (/** @type {unknown} */ v) => typeof v === 'object' && v !== null && !Array.isArray(v);
const isStr = (/** @type {unknown} */ v) => typeof v === 'string' && v.length > 0;

// The capture-only stylesheet: it removes placeholders, never data. The lab
// banner says the instance is a lab, which does not belong in a picture of the
// product. Everything else the console renders stays, the figures and the
// states included.
export const CAPTURE_CSS = [
  '[data-testid="lab-banner"] { display: none !important; }',
].join('\n');

/**
 * A question as the lab's omnibot recording compares it: lower case, runs of
 * space as one, trailing punctuation dropped (internal/assistant/provider/replay.go, normal).
 * @param {string} s
 */
export const replayNormal = (s) => s.toLowerCase().split(/\s+/).filter(Boolean).join(' ').replace(/[?.! ]+$/, '');

/**
 * The questions a shot types into omnibot that the lab's recording would refuse
 * (#2526: a recording answers only a message holding one of its prompts), each
 * named by its shot and step. A refused question never thinks, so its shot would
 * stall at its first wait; this names it before any lab starts.
 * @param {{ id: string, steps: { kind: string, target?: unknown, text?: string }[] }[]} shots
 * @param {string[]} prompts the recording's prompts
 * @returns {string[]}
 */
export function replayProblems(shots, prompts) {
  const asks = prompts.filter(Boolean).map(replayNormal);
  /** @type {string[]} */
  const out = [];
  for (const shot of shots) {
    shot.steps.forEach((step, i) => {
      if (step.kind !== 'type' || !JSON.stringify(step.target ?? '').includes('Ask omnibot')) return;
      const said = replayNormal(step.text ?? '');
      if (!asks.some((a) => said.includes(a))) {
        out.push(`${shot.id} step ${i + 1} asks omnibot "${step.text}", which the recording does not answer (it answers: ${prompts.filter(Boolean).map((p) => `"${p}"`).join(', ')})`);
      }
    });
  }
  return out;
}

/**
 * Where the pointer rests on a step's target: the middle of what it clicks or
 * hovers; the start of a field it types into; and the chevron end of a select,
 * so the value it picks stays in sight for the frame that checks it.
 * @param {string} kind
 * @param {{ x: number, y: number, width: number, height: number }} box
 * @returns {number}
 */
export function pointerX(kind, box) {
  if (kind === 'click' || kind === 'hover') return box.x + box.width / 2;
  if (kind === 'select') return Math.max(box.x + box.width - 16, box.x + box.width / 2);
  return Math.min(box.x + 24, box.x + box.width / 2);
}

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
  // spot: false lights the whole frame on this focus instead of dimming all but
  // its target, for a target that points the camera at a region rather than
  // being the one thing to read (an outage's alarm bar, with the lanes above it).
  if ('spot' in o) {
    if (o.spot !== false) problems.push(`${where}: spot takes only false, to light the whole frame`);
    else if (!('focus' in o) || o.focus === 'out') problems.push(`${where}: spot goes with a focus on a target`);
    else step.spot = false;
  }
  // expect: the text a beat's frame must hold as the beat ends, each string
  // found within what is on screen inside the frame. It goes with the focus that
  // opens the beat, so it cannot sit on a focus that leaves for the whole frame.
  if ('expect' in o) {
    const e = o.expect;
    if (!Array.isArray(e) || e.length === 0 || !e.every(isStr)) problems.push(`${where}: expect is a list of the text the frame must show`);
    else if (!('focus' in o) || o.focus === 'out') problems.push(`${where}: expect goes with a focus on a target`);
    else step.expect = e;
  }
  // lapse: N plays a wait in N seconds of the clip, the quiet seconds before
  // something changes (a loop has 9 to spend, and a console that needs 5 to
  // recover is not one to sit through). The clip is a time-lapse there, sped up
  // as much as that run's wait needs (lapseSpeed), and badged as one. Its length
  // in the clip is the shot list's, not the lab's.
  if ('fast' in o) problems.push(`${where}: fast is retired; lapse: <seconds> plays the wait in that many seconds of the clip`);
  if ('lapse' in o) {
    if (kind !== 'wait') problems.push(`${where}: lapse belongs on a wait`);
    else if (!(typeof o.lapse === 'number' && o.lapse > 0 && o.lapse <= MAX_LAPSE_SECONDS)) problems.push(`${where}: lapse is the seconds of clip the wait plays in, over 0 and at most ${MAX_LAPSE_SECONDS}`);
    else step.lapse = o.lapse;
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
    // call: an API request mid-clip, off camera, as the lab's owner: what
    // happens in the room (someone plugs the display back in) while the console
    // shows what it makes of it. Nothing on screen moves for it, so it takes no
    // focus and nothing to check.
    case 'call': {
      if (!isObj(v)) {
        problems.push(`${where} must be { put | post | patch, body }`);
        break;
      }
      const methods = ['put', 'post', 'patch'].filter((m) => m in v);
      for (const k of Object.keys(v)) if (!['put', 'post', 'patch', 'body'].includes(k)) problems.push(`${where} has unknown key "${k}"`);
      if (methods.length !== 1) problems.push(`${where} needs exactly one of put, post, patch`);
      else if (!isStr(v[methods[0]]) || !v[methods[0]].startsWith('/')) problems.push(`${where}: ${methods[0]} must be an API path starting with "/"`);
      if (!isObj(v.body)) problems.push(`${where}: needs a body mapping`);
      if ('focus' in o) problems.push(`${where}: a call is off camera, so it takes no focus`);
      if (methods.length === 1) {
        step.method = /** @type {any} */ (methods[0].toUpperCase());
        step.path = v[methods[0]];
      }
      step.body = v.body ?? {};
      break;
    }
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
        problems.push(`${where} must be { text | selector | role, timeout? } or { seconds }`);
        break;
      }
      // A wait for time itself (#2722): the lab runs on for seconds, so a
      // stretch of its record is long enough to read. On camera it is played as
      // a lapse; a focus can frame something else, never `here`.
      if ('seconds' in v) {
        if (Object.keys(v).length > 1) problems.push(`${where} waits for either a target or seconds, not both`);
        if (!(Number.isInteger(v.seconds) && v.seconds >= 1 && v.seconds <= MAX_WAIT_MS / 1000)) {
          problems.push(`${where}: seconds must be whole seconds, 1 to ${MAX_WAIT_MS / 1000}`);
        }
        if (step.focus === 'here') problems.push(`${where}: focus here needs a target, and a timed wait has none`);
        step.seconds = v.seconds;
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
      if ('seconds' in w) {
        // A pause (#2722): the lab runs on off camera, so the state the shot
        // opens on has lasted that long (a room down for ten minutes).
        if (Object.keys(w).length > 1) problems.push(`${at}: wait is either { get, field, equals } or { seconds }`);
        if (!(Number.isInteger(w.seconds) && w.seconds >= 1 && w.seconds <= MAX_PAUSE_SECONDS)) {
          problems.push(`${at}: wait.seconds must be whole seconds, 1 to ${MAX_PAUSE_SECONDS}`);
        }
        items.push({ kind: 'pause', seconds: w.seconds });
        return;
      }
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
  // health_history (#2729): the lab seeds its scenario's health storyline, so
  // the captures read a fleet that has been running (lab --health-history).
  if (isObj(lab) && lab.health_history !== undefined && typeof lab.health_history !== 'boolean') {
    problems.push("lab health_history is true or false: whether the lab seeds its scenario's health storyline");
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
      if (s.kind === 'loop') {
        const spend = authoredSeconds({ steps });
        if (spend > maxSeconds) problems.push(`${at}: its steps spend ${spend.toFixed(1)}s before the lab adds any, over the ${maxSeconds}s maximum`);
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
      lab: { scenario: lab.scenario, systems: lab.systems, omnibotReplay: lab.omnibot_replay ?? '', healthHistory: lab.health_history === true },
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
  const maxBytes = loopMaxBytes(max);
  const allowed = round(max * (1 + LOOP_TOLERANCE));
  if (!(seconds >= LOOP_MIN_SECONDS)) out.push(`shot ${id}: loop is ${seconds.toFixed(1)}s, under the ${LOOP_MIN_SECONDS}s minimum`);
  if (seconds > allowed) out.push(`shot ${id}: loop is ${seconds.toFixed(1)}s, over the ${max}s maximum and its ${Math.round(LOOP_TOLERANCE * 100)}% allowance for the lab's speed (${allowed}s)`);
  if (bytes > maxBytes) out.push(`shot ${id}: loop is ${bytes} bytes, over the ${maxBytes}-byte limit`);
  return out;
}

/**
 * What one step spends of a loop's clip, in seconds, before the lab adds any:
 * its hold, the recorder's pace for its kind, and a wait's lapse. A wait with
 * no lapse spends nothing of its own: it lasts as long as the lab takes.
 * @param {Step} step
 * @returns {number}
 */
export function stepAuthoredSeconds(step) {
  let ms = step.hold ?? 0;
  if (step.kind === 'goto') ms += PACE.load;
  if (['click', 'hover', 'type', 'select'].includes(step.kind)) ms += PACE.glide;
  if (step.kind === 'type') ms += PACE.key * (step.text ?? '').length;
  if (step.kind === 'wait') ms += (step.lapse ?? (step.seconds ?? 0)) * 1000;
  return ms / 1000;
}

/**
 * What a loop's steps spend of its clip before the lab adds any: each step's
 * own (stepAuthoredSeconds), the settle before the clip ends, and the camera's
 * ease back out when the last focus leaves it zoomed. The parse holds this to
 * the loop's maximum exactly; the recording gets LOOP_TOLERANCE on top.
 * @param {{ steps: Step[] }} shot
 * @returns {number}
 */
export function authoredSeconds({ steps }) {
  const last = steps.filter((st) => st.focus !== undefined).at(-1);
  const tail = last && last.focus !== 'out' ? CAMERA.ease + TAIL_PAD : 0;
  return steps.reduce((sum, st) => sum + stepAuthoredSeconds(st), PACE.settle / 1000 + tail);
}

/** @typedef {{ n: number, kind: string, target: string, clip: number, authored: number }} StepTime */

/**
 * Where an overrun loop's seconds went: for a loop past its maximum, the steps
 * the lab stretched most past what the shot list spends on them, so the fix
 * (a lapse on a slow wait, a shorter hold) is plain. Nothing for a loop inside it.
 * @param {string} id
 * @param {number} seconds the loop's length
 * @param {number} max its maximum
 * @param {StepTime[]} steps each step's seconds in the clip and its authored seconds
 * @returns {string[]}
 */
export function overrunReport(id, seconds, max, steps) {
  if (!(seconds > max)) return [];
  const over = steps.map((st) => ({ ...st, extra: Math.max(0, st.clip - st.authored) }));
  const added = over.reduce((sum, st) => sum + st.extra, 0);
  const worst = over.filter((st) => st.extra >= 0.2).sort((a, b) => b.extra - a.extra).slice(0, 3);
  return [
    `shot ${id}: loop is ${seconds.toFixed(1)}s, past its ${max}s maximum; the lab's speed added ${added.toFixed(1)}s, most in:`,
    ...worst.map((st) => `  step ${st.n} (${st.kind}${st.target ? ` ${st.target}` : ''}): ${st.clip.toFixed(1)}s, ${st.extra.toFixed(1)}s past its ${st.authored.toFixed(1)}s`),
  ];
}

/**
 * How much faster a lapse plays: as much as its wait needs to fit the seconds
 * the shot list gives it, never slower than real time, and at most MAX_FAST. A
 * wait past MAX_FAST times its lapse plays longer than the lapse, and the
 * loop's budget is what catches it.
 * @param {number} real the wait's seconds in real time
 * @param {number} lapse the seconds of clip the shot list gives it
 * @returns {number}
 */
export function lapseSpeed(real, lapse) {
  return Math.min(MAX_FAST, Math.max(1, real / lapse));
}

/**
 * The byte budget of a loop allowed `maxSeconds`: LOOP_MAX_BYTES up to 15
 * seconds, the same rate beyond, so a 60 second teaser may weigh 6 MB.
 * @param {number} maxSeconds
 */
export function loopMaxBytes(maxSeconds) {
  return Math.round(LOOP_MAX_BYTES * Math.max(1, maxSeconds / LOOP_BUDGET_SECONDS));
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
 * shot's files (bare names beside the manifest), alt text and pixel size, and a
 * loop's beats (when each opens and ends, and the part of the capture it frames)
 * when the recording logged them.
 * Throws on a shot the page could not use: a missing alt, a file with a path.
 * @param {{ generatedAt: string, lab: { scenario: string, systems: number }, shots: { id: string, kind: 'still' | 'loop', file: string, poster?: string, alt: string, width: number, height: number, beats?: unknown[] }[] }} m
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
      ...(s.kind === 'loop' && Array.isArray(s.beats) ? { beats: s.beats } : {}),
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
  // A pause names no resource.
  const paths = [...items.flatMap((it) => (it.kind === 'call' ? [it.path] : it.kind === 'await' ? [it.get] : [])), ...(start ? [start] : [])];
  for (const p of paths) {
    for (const m of p.matchAll(RESOURCE)) {
      const id = m[2].startsWith('{') ? m[2].slice(1, -1) : m[2];
      found.set(`${m[1]}/${id}`, { kind: KIND[/** @type {'components'} */ (m[1])], id, path: `/${m[1]}/${m[2]}` });
    }
  }
  return [...found.values()];
}

/** @typedef {{ x: number, y: number, width: number, height: number }} Box */
/** @typedef {{ t: number, box: Box | null, zoom?: number, spot?: false, follow?: true }} FocusEvent */
/** @typedef {{ at: number, dur: number, scale: number, x: number, y: number, spot: Box | null }} CameraMove */

/**
 * The frame a focus asks for: the scale that fits the box with a margin (or the
 * step's zoom), capped, centred on the box. The box is what the beat is about,
 * so it sits in the middle of the frame even near the capture's edge; past that
 * edge the frame shows the backdrop. A box bigger than the frame at a step's
 * zoom (a whole card) keeps its top and left edges in view, with the margin, as
 * a reader starts there: centring it would cut its heading. A null box is the
 * whole frame.
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
  // Along one axis: the box's middle, or, when the box overflows the frame's
  // inner span there, the point that puts its start a margin inside the frame;
  // then held so the frame never reaches past the capture (a box at its edge
  // sits off centre rather than beside empty backdrop).
  const axis = (/** @type {number} */ start, /** @type {number} */ size, /** @type {number} */ view) => {
    const span = view / scale;
    const want = size > span * (1 - 2 * CAMERA.margin) ? start - span * CAMERA.margin + span / 2 : start + size / 2;
    return Math.min(view - span / 2, Math.max(span / 2, want));
  };
  return {
    scale: round(scale),
    x: round(axis(box.x, box.width, vp.width)),
    y: round(axis(box.y, box.height, vp.height)),
  };
}

/**
 * Where the camera goes so the pointer's target is on screen as the pointer
 * reaches it: the camera follows the pointer. Null while the frame it holds
 * shows the target with a margin to spare, or shows the whole capture; else
 * the same zoom recentred on the target (held on the capture, as frameFor is).
 * @param {Box} box the target, in viewport pixels
 * @param {{ scale: number, x: number, y: number }} current the frame the camera holds
 * @param {{ width: number, height: number }} vp
 * @returns {{ scale: number, x: number, y: number } | null}
 */
export function followFrame(box, current, vp) {
  if (current.scale <= 1) return null;
  const { rect } = frameRect(current, vp);
  const m = Math.min(rect.width, rect.height) * CAMERA.margin;
  const inside =
    box.x >= rect.x + m && box.y >= rect.y + m && box.x + box.width <= rect.x + rect.width - m && box.y + box.height <= rect.y + rect.height - m;
  return inside ? null : frameFor(box, vp, current.scale);
}

/**
 * Where the camera goes for a click (a select, a typed field) with no focus of
 * its own: close enough to read what is clicked. Null when the frame it holds
 * is already at least CAMERA.clickZoom and shows the target; else the target
 * at the closer of that zoom and the one held, so a click never reads from a
 * wide frame (the architect's storyboard, #2752).
 * @param {Box} box the target, in viewport pixels
 * @param {{ scale: number, x: number, y: number }} current the frame the camera holds
 * @param {{ width: number, height: number }} vp
 * @returns {{ scale: number, x: number, y: number } | null}
 */
export function clickFrame(box, current, vp) {
  if (current.scale >= CAMERA.clickZoom) return followFrame(box, current, vp);
  return frameFor(box, vp, CAMERA.clickZoom);
}

/**
 * How much longer a step waits, past the pointer's glide, before it acts when
 * the camera moved for it (followFrame, clickFrame): long enough for the move
 * to land, so a click that closes a form is seen before the form goes.
 * @param {boolean} moved
 * @returns {number} milliseconds
 */
export function landWaitMs(moved) {
  return moved ? Math.max(0, CAMERA.ease * 1000 - PACE.glide) : 0;
}

/** @typedef {{ from: number, at: number, to: number }} BeatTimes */

/**
 * Ends each beat where the camera leaves it: at the next focus logged after the
 * beat opened (a step that waits for its target logs once the target shows, so
 * the beat runs on through that wait), or where the beat was closed when no
 * focus follows.
 * @template {{ from: number, to: number }} B
 * @param {B[]} beats
 * @param {FocusEvent[]} events the same clock as the beats
 * @returns {B[]}
 */
export function beatEnds(beats, events) {
  return beats.map((b) => {
    // A camera following the pointer (followFrame) pans within a beat; it does
    // not end it.
    const next = events.find((e) => e.t > b.from && !e.follow);
    return next ? { ...b, to: next.t } : b;
  });
}

/**
 * The second of the clip a beat's storyboard still is taken at: when its frame
 * was checked, but not before the camera has landed (it eases for CAMERA.ease)
 * and not past the beat's end.
 * @param {BeatTimes} b seconds of the finished clip
 * @returns {number}
 */
export function beatStill(b) {
  const end = Math.max(b.from, b.to - 0.05);
  return round(Math.min(end, Math.max(b.at ?? b.to, b.from + CAMERA.ease)));
}

const round = (/** @type {number} */ n) => Math.round(n * 1000) / 1000;

/**
 * The part of the capture a frame shows, in viewport pixels: what the viewer
 * sees once the camera has landed. It can reach past the capture's edge (the
 * backdrop shows there); `onScreen` is the part of it inside the capture.
 * @param {{ scale: number, x: number, y: number }} f a frame from frameFor
 * @param {Viewport} vp
 * @returns {{ rect: Box, onScreen: Box }}
 */
export function frameRect(f, vp) {
  const width = vp.width / f.scale, height = vp.height / f.scale;
  const rect = { x: round(f.x - width / 2), y: round(f.y - height / 2), width: round(width), height: round(height) };
  const x0 = Math.max(0, rect.x), y0 = Math.max(0, rect.y);
  const x1 = Math.min(vp.width, rect.x + rect.width), y1 = Math.min(vp.height, rect.y + rect.height);
  return { rect, onScreen: { x: x0, y: y0, width: round(Math.max(0, x1 - x0)), height: round(Math.max(0, y1 - y0)) } };
}

/**
 * The expected text a beat's frame does not show: each string is looked for in
 * the frame's text, its pieces joined by spaces and runs of space collapsed, so
 * a string may cross from one element to the next.
 * @param {string[]} seen the text inside the frame, in document order
 * @param {string[]} expect
 * @returns {string[]}
 */
export function missingText(seen, expect) {
  const norm = (/** @type {string} */ t) => t.replace(/\s+/g, ' ').trim();
  const all = norm(seen.join(' '));
  return expect.filter((e) => !all.includes(norm(e)));
}

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
    moves.push({ at, dur: CAMERA.ease, ...f, spot: e.spot === false ? null : spotFor(e.box, vp) });
    cur = f;
    free = at + CAMERA.ease;
  }
  if (cur.scale !== 1) {
    const at = round(Math.max(free, duration - CAMERA.ease));
    const dur = round(Math.min(CAMERA.ease, duration - at));
    if (dur > 0) moves.push({ at, dur, ...frameFor(null, vp), spot: null });
  }
  return moves;
}

/**
 * The spotlight of a move: the focus box, padded and kept on the capture,
 * which render.mjs leaves lit while it dims the rest of the frame, so the eye
 * lands on what changed. None for the whole frame, or for a box that fills
 * the capture (nothing would be left to dim).
 * @param {Box | null} box
 * @param {{ width: number, height: number }} vp
 * @returns {Box | null}
 */
export function spotFor(box, vp) {
  if (!box) return null;
  const pad = CAMERA.spotPad;
  const x = Math.max(0, box.x - pad);
  const y = Math.max(0, box.y - pad);
  const width = Math.min(vp.width, box.x + box.width + pad) - x;
  const height = Math.min(vp.height, box.y + box.height + pad) - y;
  if (width * height >= vp.width * vp.height * 0.8) return null;
  return { x: round(x), y: round(y), width: round(width), height: round(height) };
}

/**
 * The sped-up stretches of a clip, in the clip's own time, for the badge that
 * says so: a lapse shorter than MIN_LAPSE_BADGE seconds once shortened gets
 * none, since a badge that flashes reads as a glitch.
 * @param {Lapse[]} lapses in real time, ascending
 * @returns {Lapse[]}
 */
export function lapseBadges(lapses) {
  // A lapse plays exactly its seconds, so one given the shortest badged length
  // sits on the line: the comparison allows for floating-point error, or half
  // of them would lose their badge to rounding.
  return lapses
    .map((w) => ({ from: retime(w.from, lapses), to: retime(w.to, lapses), speed: w.speed, real: w.to - w.from }))
    .filter((w) => w.to - w.from >= MIN_LAPSE_BADGE - 1e-6)
    .map((w) => ({ from: round(w.from), to: round(w.to), speed: Math.round(w.speed), label: lapseLabel(w.real) }));
}

/**
 * What a lapse's clock says: the real time it covers, "+45 s" under a
 * minute and a half, then whole minutes, then whole hours.
 * @param {number} seconds real seconds
 * @returns {string}
 */
export function lapseLabel(seconds) {
  if (seconds < 90) return `+${Math.round(seconds)} s`;
  if (seconds < 90 * 60) return `+${Math.round(seconds / 60)} min`;
  return `+${Math.round(seconds / 3600)} h`;
}
export const MIN_LAPSE_BADGE = 0.6;

// The frames of a loop are screenshots at twice the viewport's pixels, taken as
// fast as the page allows (Playwright's own video is CSS-pixel sized, so a 2x
// recording of it is the 1x frame padded with gray). Each frame shows from its
// own time to the next one's; the first shows from the clip's start.
export const FRAME_FALLBACK_SECONDS = 0.04;

/**
 * A loop's frames thinned to at most one per `gap` seconds of clip, the first
 * and last always kept. A lapse retimes its frames into a fraction of their
 * real span (180 s at 300x is 0.6 s), so thousands of them land a ten-thousandth
 * of a second apart; joined as they are, each still took the join's smallest
 * duration and the lapse played ten times slower than its speed. The encode
 * keeps one frame per 1/25 s anyway, so nothing visible is lost.
 * @template {{ t: number }} F
 * @param {F[]} frames ascending in t
 * @param {number} gap seconds
 * @returns {F[]}
 */
export function thinFrames(frames, gap) {
  if (frames.length <= 2) return [...frames];
  const kept = [frames[0]];
  for (let i = 1; i < frames.length - 1; i++) {
    if (frames[i].t - kept[kept.length - 1].t >= gap) kept.push(frames[i]);
  }
  kept.push(frames[frames.length - 1]);
  return kept;
}

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
  return last && last.box ? CAMERA.ease + TAIL_PAD : 0;
}
// The beat past the camera's last ease before the loop's seam.
const TAIL_PAD = 0.15;
