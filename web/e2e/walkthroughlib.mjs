// @ts-check
// The pure core of the walkthrough recorder (#924): the script format, its
// strict validation, and the budget. Plain ESM so the recorder runs it with no
// build step; its types are the JSDoc below, checked by tsc (web/tsconfig.json
// checkJs) and read from here by the unit tests (web/src/walkthrough-script.test.ts).
//
// A walkthrough is web/e2e/walkthroughs/<id>.yaml:
//
//   title: Read a simulated component's status      # required
//   description: One sentence on what it shows.     # optional
//   steps:                                          # required, at least one
//     - goto: /simulation                           # a console path (under /web)
//       caption: The Simulation page lists ...      # required on every step
//     - click: '[data-testid="sim-row"] >> nth=0'   # a Playwright selector, or
//       caption: ...
//     - click: { role: button, name: Close }        # an accessible role and name
//       caption: ...
//     - type: { target: '#login-username', text: dev }
//     - select: { target: '#product', value: generic-display }
//     - press: Escape
//     - wait: { text: Hits, timeout: 10000 }        # a visible result: text, a
//       caption: ...                                # selector, or a role and name
//
// A script may not end on an action (click, type, select, press): its last step
// is a wait for the result the action should produce, or nothing proves it.
//
// Every step also takes `hold` (extra milliseconds to linger after it, for a
// result worth reading) and `poster: true` (its still is the poster frame, at
// most one step; default the last). Unknown keys, unknown kinds, and a step with two kinds
// fail, naming the file and the step.

import { load } from 'js-yaml';

/** @typedef {'goto' | 'click' | 'type' | 'select' | 'press' | 'wait'} Kind */
/**
 * A selector string, an accessible role and name, or (wait only) text or a
 * selector. `exact` applies to a name or text match only.
 * @typedef {string | { role: string, name?: string, exact?: boolean } | { text: string, exact?: boolean } | { selector: string }} Target
 */
/**
 * @typedef {object} Step
 * @property {number} n
 * @property {Kind} kind
 * @property {string} caption
 * @property {number} hold
 * @property {boolean} poster
 * @property {string} [path]
 * @property {string} [key]
 * @property {Target} [target]
 * @property {string} [text]
 * @property {string} [value]
 * @property {number} [timeout]
 */
/** @typedef {{ title: string, description: string, steps: Step[], poster: number }} Walkthrough */
/**
 * Where a step failed: waiting for its target to be visible, scrolling to it,
 * acting on it, loading a page, or taking its still; with that phase's timeout
 * (null where the phase has none).
 * @typedef {{ name: 'load' | 'visible' | 'scroll' | 'act' | 'still', timeoutMs: number | null }} Phase
 */

/** @type {Kind[]} */
export const KINDS = ['goto', 'click', 'type', 'select', 'press', 'wait'];
// The kinds that do something; a script may not end on one.
/** @type {string[]} */
const ACTIONS = ['click', 'type', 'select', 'press'];
/** @type {string[]} */
const KIND_NAMES = KINDS;

// The budget the architect ruled (#884): under 60 seconds and 10 MB of mp4.
export const MAX_SECONDS = 60;
export const MAX_BYTES = 10_000_000;

export const DEFAULT_WAIT_MS = 10_000;
const MAX_WAIT_MS = 60_000;
const MAX_HOLD_MS = 10_000;
const COMMON = ['caption', 'hold', 'poster'];
const TOP = ['title', 'description', 'steps'];

/** @type {(v: unknown) => v is Record<string, any>} */
const isObj = (v) => v !== null && typeof v === 'object' && !Array.isArray(v);
/** @type {(v: unknown) => v is string} */
const isStr = (v) => typeof v === 'string' && v.trim() !== '';

// A target is a selector string, or { role, name, exact? }, or (for wait only)
// { text, exact? } / { selector }. Returns the problems, empty when valid.
/**
 * @param {unknown} t
 * @param {string} where
 * @param {boolean} allowText
 * @returns {string[]}
 */
function targetProblems(t, where, allowText) {
  if (isStr(t)) return [];
  if (!isObj(t)) return [`${where} must be a selector string or an object`];
  const keys = Object.keys(t);
  if ('role' in t) {
    const bad = keys.filter((k) => !['role', 'name', 'exact'].includes(k));
    const out = bad.map((k) => `${where} has unknown key "${k}"`);
    if (!isStr(t.role)) out.push(`${where}.role must be a non-empty string`);
    if ('name' in t && !isStr(t.name)) out.push(`${where}.name must be a non-empty string`);
    if ('exact' in t && typeof t.exact !== 'boolean') out.push(`${where}.exact must be true or false`);
    return out;
  }
  if (allowText && ('text' in t || 'selector' in t)) {
    const which = 'text' in t ? 'text' : 'selector';
    const known = which === 'text' ? ['text', 'exact'] : ['selector'];
    const out = keys.filter((k) => !known.includes(k)).map((k) => `${where} has unknown key "${k}"`);
    if (!isStr(t[which])) out.push(`${where}.${which} must be a non-empty string`);
    if ('exact' in t && typeof t.exact !== 'boolean') out.push(`${where}.exact must be true or false`);
    return out;
  }
  return [`${where} needs ${allowText ? 'text, selector, or role' : 'a selector string or role'}`];
}

// Validates one step; returns [normalized step | null, problems].
/**
 * @param {unknown} raw
 * @param {number} n
 * @returns {[Step | null, string[]]}
 */
function validateStep(raw, n) {
  const at = `step ${n}`;
  if (!isObj(raw)) return [null, [`${at} must be a mapping`]];
  const kinds = Object.keys(raw).filter((k) => KIND_NAMES.includes(k));
  const unknown = Object.keys(raw).filter((k) => !KIND_NAMES.includes(k) && !COMMON.includes(k));
  if (kinds.length === 0) {
    const named = unknown.length ? ` (unknown kind or key ${unknown.map((k) => `"${k}"`).join(', ')})` : '';
    return [null, [`${at} has no kind: one of ${KINDS.join(', ')}${named}`]];
  }
  if (kinds.length > 1) return [null, [`${at} has more than one kind: ${kinds.join(', ')}`]];
  const kind = /** @type {Kind} */ (kinds[0]);
  const where = `${at} (${kind})`;
  const problems = unknown.map((k) => `${where} has unknown key "${k}"`);
  if (!isStr(raw.caption)) problems.push(`${where} needs a caption`);
  if ('hold' in raw && !(Number.isInteger(raw.hold) && raw.hold >= 0 && raw.hold <= MAX_HOLD_MS)) {
    problems.push(`${where}: hold must be whole milliseconds, 0 to ${MAX_HOLD_MS}`);
  }
  if ('poster' in raw && raw.poster !== true) problems.push(`${where}: poster is true or absent`);
  const v = raw[kind];
  /** @type {Step} */
  const step = { n, kind, caption: raw.caption, hold: raw.hold ?? 0, poster: raw.poster === true };
  switch (kind) {
    case 'goto':
      if (!isStr(v) || !v.startsWith('/')) problems.push(`${where} must be a console path starting with "/"`);
      step.path = v;
      break;
    case 'click':
      problems.push(...targetProblems(v, where, false));
      step.target = v;
      break;
    case 'press':
      if (!isStr(v)) problems.push(`${where} must name a key (Enter, Escape, Tab, ...)`);
      step.key = v;
      break;
    case 'type':
    case 'select': {
      const field = kind === 'type' ? 'text' : 'value';
      if (!isObj(v)) {
        problems.push(`${where} must be { target, ${field} }`);
        break;
      }
      for (const k of Object.keys(v)) if (k !== 'target' && k !== field) problems.push(`${where} has unknown key "${k}"`);
      problems.push(...targetProblems(v.target, `${where} target`, false));
      if (typeof v[field] !== 'string') problems.push(`${where} needs ${field}`);
      step.target = v.target;
      step[field] = v[field];
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

// Validates a parsed walkthrough document. Throws one Error listing every
// problem, each prefixed with the file; returns the normalized walkthrough.
/**
 * @param {unknown} doc
 * @param {string} file
 * @returns {Walkthrough}
 */
export function validateWalkthrough(doc, file) {
  const problems = [];
  if (!isObj(doc)) throw new Error(`${file}: a walkthrough is a mapping with title and steps`);
  for (const k of Object.keys(doc)) if (!TOP.includes(k)) problems.push(`unknown key "${k}"`);
  if (!isStr(doc.title)) problems.push('needs a title');
  if ('description' in doc && !isStr(doc.description)) problems.push('description must be a non-empty string');
  /** @type {Step[]} */
  const steps = [];
  if (!Array.isArray(doc.steps) || doc.steps.length === 0) {
    problems.push('needs steps: a list of at least one step');
  } else {
    doc.steps.forEach((raw, i) => {
      const [step, ps] = validateStep(raw, i + 1);
      problems.push(...ps);
      if (step) steps.push(step);
    });
  }
  const last = steps[steps.length - 1];
  if (last && last.n === doc.steps.length && ACTIONS.includes(last.kind)) {
    problems.push(`step ${last.n} (${last.kind}) is the last step: end on a wait for the result it should produce, or the walkthrough proves nothing`);
  }
  const posters = steps.filter((s) => s.poster).map((s) => s.n);
  if (posters.length > 1) problems.push(`only one step may be the poster (steps ${posters.join(', ')} are)`);
  if (problems.length) throw new Error(problems.map((p) => `${file}: ${p}`).join('\n'));
  return { title: doc.title, description: doc.description ?? '', steps, poster: posters[0] ?? steps.length };
}

// Parses and validates a walkthrough's YAML text; a syntax error names the file.
/**
 * @param {string} text
 * @param {string} file
 * @returns {Walkthrough}
 */
export function parseWalkthrough(text, file) {
  let doc;
  try {
    doc = load(text);
  } catch (e) {
    throw new Error(`${file}: not valid YAML: ${String(/** @type {Error} */ (e).message).split('\n')[0]}`);
  }
  return validateWalkthrough(doc, file);
}

// What a target names, for logs and failure messages.
/**
 * @param {Target} t
 * @returns {string}
 */
export function describeTarget(t) {
  if (typeof t === 'string') return `selector ${JSON.stringify(t)}`;
  if ('role' in t) return `${t.role}${t.name ? ` named ${JSON.stringify(t.name)}` : ''}`;
  if ('text' in t) return `text ${JSON.stringify(t.text)}`;
  return `selector ${JSON.stringify(t.selector)}`;
}

// What the step was doing when the phase failed.
/**
 * @param {Step} step
 * @param {Phase} phase
 * @returns {string}
 */
function describePhase(step, { name, timeoutMs }) {
  const waited = `waited ${(timeoutMs ?? 0) / 1000}s`;
  const target = step.target === undefined ? '' : describeTarget(step.target);
  switch (name) {
    case 'load':
      return `${waited} for the console path ${step.path} to load`;
    case 'visible':
      return `${waited} for ${target} to be visible`;
    case 'scroll':
      return `${waited} to scroll ${target} into view`;
    case 'still':
      return 'could not take its still';
    case 'act':
      switch (step.kind) {
        case 'press':
          return `could not press the key ${step.key}`;
        case 'type':
          return `${waited} to type into ${target}`;
        case 'select':
          return `${waited} to select ${JSON.stringify(step.value)} in ${target}`;
        default:
          return `${waited} to click ${target}`;
      }
  }
}

// The failure line a broken step prints: the walkthrough, the step number and
// kind, the phase that failed, what it waited for and for how long, and the
// underlying reason.
/**
 * @param {string} id
 * @param {Step} step
 * @param {Phase} phase
 * @param {unknown} [reason]
 * @returns {string}
 */
export function stepFailure(id, step, phase, reason) {
  const first = String(reason ?? '').split('\n')[0].trim();
  return `walkthrough ${id}: step ${step.n} (${step.kind}) failed at ${phase.name}: ${describePhase(step, phase)}${first ? ` (${first})` : ''}`;
}

/** @param {number} b */
const size = (b) => (b >= 10_000 ? `${(b / 1e6).toFixed(2)} MB` : `${b} bytes`);

// The limits the budget gate applies: the ruled ones, which the environment
// (OG_WALKTHROUGH_MAX_SECONDS / OG_WALKTHROUGH_MAX_BYTES, for the recorder's own
// test) may lower but never raise. A value that is not a positive number is
// ignored.
/**
 * @param {Record<string, string | undefined>} env
 * @returns {{ maxSeconds: number, maxBytes: number }}
 */
export function budgetLimits(env) {
  /** @type {(v: string | undefined, max: number) => number} */
  const lower = (v, max) => {
    const n = v === undefined || v.trim() === '' ? NaN : Number(v);
    return Number.isFinite(n) && n > 0 ? Math.min(n, max) : max;
  };
  return { maxSeconds: lower(env.OG_WALKTHROUGH_MAX_SECONDS, MAX_SECONDS), maxBytes: lower(env.OG_WALKTHROUGH_MAX_BYTES, MAX_BYTES) };
}

// The budget verdict for one recorded walkthrough: empty when it fits, else one
// line per limit exceeded, naming which. A duration that is not a positive
// number (ffprobe printed nothing) was never measured, and fails.
/**
 * @param {string} id
 * @param {{ seconds: number, bytes: number }} measured
 * @param {{ maxSeconds?: number, maxBytes?: number }} [limits]
 * @returns {string[]}
 */
export function budgetProblems(id, { seconds, bytes }, { maxSeconds = MAX_SECONDS, maxBytes = MAX_BYTES } = {}) {
  if (!(Number.isFinite(seconds) && seconds > 0)) return [`walkthrough ${id}: no duration measured (got ${seconds}): the recording could not be read`];
  const out = [];
  if (!(seconds < maxSeconds)) out.push(`walkthrough ${id}: over budget: duration ${seconds.toFixed(1)}s, the limit is under ${maxSeconds}s`);
  if (!(bytes < maxBytes)) {
    out.push(`walkthrough ${id}: over budget: size ${size(bytes)}, the limit is under ${size(maxBytes)}`);
  }
  return out;
}

// The still for step n: step-01.png ... step-99.png.
/** @param {number} n */
export const stillName = (n) => `step-${String(n).padStart(2, '0')}.png`;

// A walkthrough's id names its script, its output directory and its mp4, so it
// is kept to lowercase letters, digits and hyphens. Null when valid.
/**
 * @param {string} id
 * @returns {string | null}
 */
export function idProblem(id) {
  return /^[a-z0-9-]+$/.test(id) ? null : `walkthrough id ${JSON.stringify(id)} must be lowercase letters, digits and hyphens`;
}
