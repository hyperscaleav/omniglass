// The pure core of the walkthrough recorder (#924): the script format, its
// strict validation, and the budget. Plain ESM so the recorder runs it with no
// build step; the unit tests (web/src/walkthrough-script.test.ts) import it
// through walkthroughlib.d.mts.
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
// Every step also takes `hold` (extra milliseconds to linger after it, for a
// result worth reading) and `poster: true` (its still is the poster frame, at
// most one step; default the last). Unknown keys, unknown kinds, and a step with two kinds
// fail, naming the file and the step.

import { load } from 'js-yaml';

export const KINDS = ['goto', 'click', 'type', 'select', 'press', 'wait'];

// The budget the architect ruled (#884): under 60 seconds and 10 MB of mp4.
export const MAX_SECONDS = 60;
export const MAX_BYTES = 10_000_000;

export const DEFAULT_WAIT_MS = 10_000;
const MAX_WAIT_MS = 60_000;
const MAX_HOLD_MS = 10_000;
const COMMON = ['caption', 'hold', 'poster'];
const TOP = ['title', 'description', 'steps'];

const isObj = (v) => v !== null && typeof v === 'object' && !Array.isArray(v);
const isStr = (v) => typeof v === 'string' && v.trim() !== '';

// A target is a selector string, or { role, name, exact? }, or (for wait only)
// { text } / { selector }. Returns the problems, empty when valid.
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
    const out = keys.filter((k) => k !== which && k !== 'exact').map((k) => `${where} has unknown key "${k}"`);
    if (!isStr(t[which])) out.push(`${where}.${which} must be a non-empty string`);
    if ('exact' in t && typeof t.exact !== 'boolean') out.push(`${where}.exact must be true or false`);
    return out;
  }
  return [`${where} needs ${allowText ? 'text, selector, or role' : 'a selector string or role'}`];
}

// Validates one step; returns [normalized step | null, problems].
function validateStep(raw, n) {
  const at = `step ${n}`;
  if (!isObj(raw)) return [null, [`${at} must be a mapping`]];
  const kinds = Object.keys(raw).filter((k) => KINDS.includes(k));
  const unknown = Object.keys(raw).filter((k) => !KINDS.includes(k) && !COMMON.includes(k));
  if (kinds.length === 0) {
    const named = unknown.length ? ` (unknown kind or key ${unknown.map((k) => `"${k}"`).join(', ')})` : '';
    return [null, [`${at} has no kind: one of ${KINDS.join(', ')}${named}`]];
  }
  if (kinds.length > 1) return [null, [`${at} has more than one kind: ${kinds.join(', ')}`]];
  const kind = kinds[0];
  const where = `${at} (${kind})`;
  const problems = unknown.map((k) => `${where} has unknown key "${k}"`);
  if (!isStr(raw.caption)) problems.push(`${where} needs a caption`);
  if ('hold' in raw && !(Number.isInteger(raw.hold) && raw.hold >= 0 && raw.hold <= MAX_HOLD_MS)) {
    problems.push(`${where}: hold must be whole milliseconds, 0 to ${MAX_HOLD_MS}`);
  }
  if ('poster' in raw && raw.poster !== true) problems.push(`${where}: poster is true or absent`);
  const v = raw[kind];
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
      step.target = target;
      step.timeout = timeout ?? DEFAULT_WAIT_MS;
      break;
    }
  }
  return [step, problems];
}

// Validates a parsed walkthrough document. Throws one Error listing every
// problem, each prefixed with the file; returns the normalized walkthrough.
export function validateWalkthrough(doc, file) {
  const problems = [];
  if (!isObj(doc)) throw new Error(`${file}: a walkthrough is a mapping with title and steps`);
  for (const k of Object.keys(doc)) if (!TOP.includes(k)) problems.push(`unknown key "${k}"`);
  if (!isStr(doc.title)) problems.push('needs a title');
  if ('description' in doc && !isStr(doc.description)) problems.push('description must be a non-empty string');
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
  const posters = steps.filter((s) => s.poster).map((s) => s.n);
  if (posters.length > 1) problems.push(`only one step may be the poster (steps ${posters.join(', ')} are)`);
  if (problems.length) throw new Error(problems.map((p) => `${file}: ${p}`).join('\n'));
  return { title: doc.title, description: doc.description ?? '', steps, poster: posters[0] ?? steps.length };
}

// Parses and validates a walkthrough's YAML text; a syntax error names the file.
export function parseWalkthrough(text, file) {
  let doc;
  try {
    doc = load(text);
  } catch (e) {
    throw new Error(`${file}: not valid YAML: ${String(e.message).split('\n')[0]}`);
  }
  return validateWalkthrough(doc, file);
}

// What a target names, for logs and failure messages.
export function describeTarget(t) {
  if (typeof t === 'string') return `selector ${JSON.stringify(t)}`;
  if (t.role) return `${t.role}${t.name ? ` named ${JSON.stringify(t.name)}` : ''}`;
  if (t.text) return `text ${JSON.stringify(t.text)}`;
  return `selector ${JSON.stringify(t.selector)}`;
}

// What a step waits on before it acts, for the failure message.
export function describeStep(step) {
  switch (step.kind) {
    case 'goto':
      return `the console path ${step.path} to load`;
    case 'press':
      return `the key ${step.key}`;
    case 'wait':
      return `${describeTarget(step.target)} to be visible`;
    default:
      return `${describeTarget(step.target)} to be visible`;
  }
}

// The failure line a broken step prints: the walkthrough, the step number and
// kind, what it waited for and for how long, and the underlying reason.
export function stepFailure(id, step, timeoutMs, reason) {
  const first = String(reason ?? '').split('\n')[0].trim();
  return `walkthrough ${id}: step ${step.n} (${step.kind}) failed: waited ${timeoutMs / 1000}s for ${describeStep(step)}${first ? ` (${first})` : ''}`;
}

const size = (b) => (b >= 10_000 ? `${(b / 1e6).toFixed(2)} MB` : `${b} bytes`);

// The budget verdict for one recorded walkthrough: empty when it fits, else one
// line per limit exceeded, naming which.
export function budgetProblems(id, { seconds, bytes }, { maxSeconds = MAX_SECONDS, maxBytes = MAX_BYTES } = {}) {
  const out = [];
  if (!(seconds < maxSeconds)) out.push(`walkthrough ${id}: over budget: duration ${seconds.toFixed(1)}s, the limit is under ${maxSeconds}s`);
  if (!(bytes < maxBytes)) {
    out.push(`walkthrough ${id}: over budget: size ${size(bytes)}, the limit is under ${size(maxBytes)}`);
  }
  return out;
}

// The still for step n: step-01.png ... step-99.png.
export const stillName = (n) => `step-${String(n).padStart(2, '0')}.png`;
