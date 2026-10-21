// @ts-check
// The run digest (#965): one probe run against a stack (web/e2e/probe.spec.ts)
// writes what it saw as raw events, one JSON object per line; reduce() folds
// them into one small digest.json, the only thing an agent reads about the
// run. Raw logs are never handed to a model: the reduction is deterministic,
// grouped, ranked, and capped in bytes, so a run's cost to read is bounded
// however much the run saw. Pure, with no dependencies, so the unit tests and
// the probe's node load it alike.
//
// Raw events (every one carries `surface`, the route or walkthrough it was
// seen on, as "route:<path>" or "walkthrough:<id>"):
//   { kind: 'plan', surfaces }                          how many surfaces the run means to drive
//   { kind: 'visit' }                                   a surface was driven
//   { kind: 'console', text }                           a console.error line
//   { kind: 'pageerror', text }                         an uncaught page error
//   { kind: 'response', method, url, status, ms }       an API response and its time
//   { kind: 'vitals', lcp, cls }                        the surface's web vitals
//   { kind: 'axe', id, impact, help, nodes }            one accessibility rule violated
//   { kind: 'walkthrough', ok, step, reason, still }    a replayed walkthrough's outcome, and the
//                                                       frame it failed on (beside digest.json)

/**
 * @typedef {{ apiP95Ms: number, lcpMs: number, cls: number, maxFindings: number, maxTextChars: number, maxBytes: number }} Budgets
 * @typedef {{ kind: string, key: string, severity: 'high'|'medium'|'low', count: number, surfaces: string[], detail: string }} Finding
 * @typedef {{ route: string, count: number, p95Ms: number }} RouteTiming
 * @typedef {{ version: 1, surfaces: number, requests: number, findings: Finding[], truncated: number, slowest: RouteTiming[] }} Digest
 */

/** @type {Budgets} */
export const DEFAULT_BUDGETS = Object.freeze({
  apiP95Ms: 1000,
  lcpMs: 2500,
  cls: 0.1,
  maxFindings: 40,
  maxTextChars: 200,
  maxBytes: 16 * 1024,
});

// How many surfaces a finding lists, and how many of the slowest routes the
// digest carries regardless of budget (the context a reader needs to judge a
// breach, or to notice one coming).
const MAX_SURFACES = 5;
const MAX_SLOWEST = 8;

const SEVERITY_RANK = { high: 0, medium: 1, low: 2 };

const UUID = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/gi;

/**
 * Matchers for the API's own path templates (api/openapi.json, generated from
 * the Go structs), so a route is grouped the way the API declares it rather
 * than by guessing which segment is an address: `/components/{name}/health`
 * folds every component's name, uuid or not.
 * @param {string[]} templates the OpenAPI paths, server prefix included
 * @returns {{ template: string, re: RegExp }[]}
 */
export function routeMatchers(templates) {
  return templates
    .map((t) => ({ template: t, re: new RegExp('^' + t.replace(/[.*+?^$()|[\]\\]/g, '\\$&').replace(/\{[^}]+\}/g, '[^/]+?') + '$') }))
    // The most literal template wins, so /components:search beats /components/{name}.
    .sort((a, b) => (b.template.match(/[^{}]/g)?.length ?? 0) - (a.template.match(/[^{}]/g)?.length ?? 0));
}

/**
 * The route an API URL is grouped under: its OpenAPI template when one
 * matches, else the path with ids and numbers folded, so a thousand reads of
 * a thousand components are one route.
 * @param {string} url
 * @param {{ template: string, re: RegExp }[]} [matchers]
 */
export function routeKey(url, matchers = []) {
  let path = url;
  try {
    path = new URL(url, 'http://x').pathname;
  } catch {
    path = url.split('?')[0];
  }
  for (const m of matchers) if (m.re.test(path)) return m.template;
  return path
    .replace(UUID, ':id')
    .split('/')
    .map((seg) => (/^\d+$/.test(seg) ? ':n' : seg))
    .join('/');
}

/**
 * A message with its run-specific parts folded, so the same fault on two rows
 * is one finding, and bounded in length.
 * @param {string} text
 * @param {number} max
 */
export function normalizeText(text, max) {
  const t = String(text).replace(UUID, '<id>').replace(/\b\d{2,}\b/g, '<n>').replace(/\s+/g, ' ').trim();
  return t.length > max ? t.slice(0, max - 1) + '…' : t;
}

/** @param {number[]} values */
export function p95(values) {
  if (values.length === 0) return 0;
  const sorted = [...values].sort((a, b) => a - b);
  return sorted[Math.min(sorted.length - 1, Math.ceil(sorted.length * 0.95) - 1)];
}

/**
 * The digest as the file holds it, so the byte cap is measured on exactly
 * what a reader loads.
 * @param {Digest} digest
 */
export const serialize = (digest) => JSON.stringify(digest, null, 2) + '\n';

/** @param {string} s */
const byteLength = (s) => new TextEncoder().encode(s).length;

/**
 * Fold raw events into the digest. The result depends only on the multiset of
 * events, never their order: the probe's listeners are asynchronous, so two
 * runs of the same console interleave differently.
 * @param {any[]} events
 * @param {Partial<Budgets>} [overrides]
 * @param {string[]} [templates] the API's path templates (routeMatchers)
 * @returns {Digest}
 */
export function reduce(events, overrides = {}, templates = []) {
  const b = { ...DEFAULT_BUDGETS, ...overrides };
  const matchers = routeMatchers(templates);
  /** @type {Map<string, { kind: string, key: string, severity: Finding['severity'], count: number, surfaces: Set<string>, detail: string, nodes: number }>} */
  const found = new Map();
  const add = (/** @type {string} */ kind, /** @type {string} */ key, /** @type {Finding['severity']} */ severity, /** @type {string} */ surface, /** @type {string} */ detail, nodes = 0) => {
    const id = `${kind}:${key}`;
    let f = found.get(id);
    if (!f) {
      f = { kind, key, severity, count: 0, surfaces: new Set(), detail, nodes: 0 };
      found.set(id, f);
    }
    f.count++;
    f.nodes += nodes;
    // One key, one detail, whatever order its events came in: the smallest.
    if (detail < f.detail) f.detail = detail;
    if (surface) f.surfaces.add(surface);
  };
  let planned = 0;

  const surfaces = new Set();
  /** @type {Map<string, number[]>} */
  const timings = new Map();
  /** @type {Map<string, Set<string>>} */
  const routeSurfaces = new Map();
  let requests = 0;

  for (const e of events) {
    if (e.kind === 'visit' && e.surface) surfaces.add(e.surface);
    switch (e.kind) {
      case 'plan':
        planned = Math.max(planned, Number(e.surfaces) || 0);
        break;
      case 'console': {
        const t = normalizeText(e.text, b.maxTextChars);
        add('console-error', t, 'medium', e.surface, t);
        break;
      }
      case 'pageerror': {
        const t = normalizeText(e.text, b.maxTextChars);
        add('page-error', t, 'high', e.surface, t);
        break;
      }
      case 'response': {
        requests++;
        const route = `${e.method} ${routeKey(e.url, matchers)}`;
        if (typeof e.ms === 'number') {
          if (!timings.has(route)) timings.set(route, []);
          timings.get(route)?.push(e.ms);
          if (!routeSurfaces.has(route)) routeSurfaces.set(route, new Set());
          routeSurfaces.get(route)?.add(e.surface);
        }
        if (e.status >= 500) add('http-5xx', `${route} ${e.status}`, 'high', e.surface, `${route} answered ${e.status}`);
        else if (e.status >= 400) add('http-4xx', `${route} ${e.status}`, 'low', e.surface, `${route} answered ${e.status}`);
        break;
      }
      case 'vitals':
        if (typeof e.lcp === 'number' && e.lcp > b.lcpMs) add('slow-paint', e.surface, 'medium', e.surface, `largest contentful paint ${Math.round(e.lcp)}ms, budget ${b.lcpMs}ms`);
        if (typeof e.cls === 'number' && e.cls > b.cls) add('layout-shift', e.surface, 'low', e.surface, `cumulative layout shift ${e.cls.toFixed(3)}, budget ${b.cls}`);
        break;
      case 'axe': {
        const sev = e.impact === 'critical' || e.impact === 'serious' ? 'medium' : 'low';
        add('a11y', e.id, sev, e.surface, normalizeText(`${e.impact ?? 'unknown'}: ${e.help ?? e.id}`, b.maxTextChars), Number(e.nodes) || 0);
        break;
      }
      case 'walkthrough':
        if (!e.ok) add('walkthrough-failed', e.surface, 'high', e.surface, `step ${e.step ?? '?'}: ${normalizeText(e.reason ?? 'failed', b.maxTextChars)}${e.still ? ` (frame: ${e.still})` : ''}`);
        break;
    }
  }

  /** @type {RouteTiming[]} */
  const routes = [...timings].map(([route, ms]) => ({ route, count: ms.length, p95Ms: Math.round(p95(ms)) }));
  for (const r of routes) {
    if (r.p95Ms > b.apiP95Ms) {
      for (const s of routeSurfaces.get(r.route) ?? []) add('slow-route', r.route, 'medium', s, `p95 ${r.p95Ms}ms over ${r.count} calls, budget ${b.apiP95Ms}ms`);
      const f = found.get(`slow-route:${r.route}`);
      if (f) f.count = r.count;
    }
  }
  // A surface planned and never visited is a hole in the evidence, which a
  // reader of the digest alone would otherwise never see.
  if (planned > surfaces.size) {
    const missing = planned - surfaces.size;
    add('probe-incomplete', 'unvisited', 'high', 'probe', `${missing} of ${planned} planned surfaces were never driven; the probe log says why`);
  }
  const slowest = routes.sort((x, y) => y.p95Ms - x.p95Ms || x.route.localeCompare(y.route)).slice(0, MAX_SLOWEST);

  /** @type {Finding[]} */
  const all = [...found.values()].map((f) => ({
    kind: f.kind,
    key: f.key,
    severity: f.severity,
    count: f.count,
    surfaces: [...f.surfaces].sort().slice(0, MAX_SURFACES),
    detail: f.kind === 'a11y' ? `${f.detail} (${f.nodes} nodes across ${f.surfaces.size} surfaces)` : f.detail,
  }));
  const ranked = all.sort(
    (x, y) => SEVERITY_RANK[x.severity] - SEVERITY_RANK[y.severity] || y.count - x.count || `${x.kind}:${x.key}`.localeCompare(`${y.kind}:${y.key}`),
  );
  /** @type {Digest} */
  const digest = { version: 1, surfaces: surfaces.size, requests, findings: ranked.slice(0, b.maxFindings), truncated: Math.max(0, ranked.length - b.maxFindings), slowest };
  // The byte cap outranks the count cap: drop the lowest-ranked findings until
  // the digest fits, counting every one dropped.
  while (digest.findings.length > 0 && byteLength(serialize(digest)) > b.maxBytes) {
    digest.findings.pop();
    digest.truncated++;
  }
  return digest;
}

/**
 * Parse a raw.jsonl body. A line that is not JSON is itself a finding of the
 * harness, kept as a console-style event so it is seen rather than dropped.
 * @param {string} text
 */
export function parseRaw(text) {
  return text
    .split('\n')
    .filter((l) => l.trim() !== '')
    .map((l) => {
      try {
        return JSON.parse(l);
      } catch {
        return { kind: 'console', surface: 'probe', text: `unparseable probe line: ${l}` };
      }
    });
}
