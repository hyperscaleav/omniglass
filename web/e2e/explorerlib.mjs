// @ts-check
// The synthetic user's pure core (#966): which mission today's run takes,
// which lenses it brings, the one shape every lens finding has, and what
// filing the findings means against the issues already open. The /explore
// skill drives these through explorer.mjs; every decision a run makes that
// can be made without a model is made here, so it is tested rather than
// re-derived by an agent each morning. No dependencies: the unit tests and
// the command load it alike.
//
// State lives on GitHub, not on disk, since every run is a fresh session: the
// run log is the marker lines the weekly epic's comments carry
// (`explorer-run: <date> mission=<id> lenses=<a>,<b>`), and an issue's
// identity is the marker line its body ends with (`explorer-key: <key>`).

/**
 * @typedef {{ id: string, persona: string, user: string, goal: string, scenario: string, lenses: string[], surfaces: string[], done: string }} Mission
 * @typedef {{ date: string, mission: string, lenses: string[] }} Run
 * @typedef {{ lens: string, kind: 'bug'|'suggestion', severity: 'high'|'medium'|'low', key: string, area: string, surface: string, claim: string, evidence: string, failure_scenario: string, repro?: string }} Finding
 * @typedef {{ number: number, title: string, body: string, state?: string, createdAt?: string }} Issue
 */

export const LENSES = Object.freeze(['performance', 'design', 'ux', 'ai-friendliness', 'accessibility']);
// The lens a run credits its own findings to (and the digest's): the
// explorer's, not a specialist's.
export const EXPLORER = 'explorer';
// A run brings at most this many lenses: each one is a subagent's worth of
// tokens, and two well-aimed lenses find more than five skimming.
export const MAX_LENSES = 2;
// A lens unused this many days rides along on the next run whatever the
// mission asks for, so no lens goes dark because no mission names it.
export const LENS_BACKSTOP_DAYS = 7;
// Suggestions filed per weekly epic: a cap on what the architect's weekly
// triage has to read, with the most severe first.
export const WEEKLY_SUGGESTION_CAP = 15;

const RUN_LINE = /^explorer-run: (\d{4}-\d{2}-\d{2}) mission=([a-z0-9-]+) lenses=([a-z0-9,-]*)\s*$/gm;
const KEY_LINE = /^explorer-key: (\S+)\s*$/m;
const KEY = /^[a-z0-9][a-z0-9:._/-]{2,119}$/;
const DATE = /^\d{4}-\d{2}-\d{2}$/;

/** @param {string} date @returns {number} */
const day = (date) => Date.parse(`${date}T00:00:00Z`) / 86_400_000;

/**
 * Check a missions document.
 * @param {any} doc
 * @returns {string[]} problems, empty when the document is good
 */
export function missionProblems(doc) {
  const out = [];
  if (!doc || !Array.isArray(doc.missions) || doc.missions.length === 0) return ['missions: a non-empty `missions` list is required'];
  const seen = new Set();
  for (const [i, m] of doc.missions.entries()) {
    const at = `missions[${i}]${m?.id ? ` (${m.id})` : ''}`;
    for (const f of ['id', 'persona', 'user', 'goal', 'scenario', 'done']) if (typeof m?.[f] !== 'string' || m[f].trim() === '') out.push(`${at}: \`${f}\` is required`);
    if (typeof m?.id === 'string' && !/^[a-z0-9][a-z0-9-]*$/.test(m.id)) out.push(`${at}: id must be lowercase letters, digits and dashes`);
    if (seen.has(m?.id)) out.push(`${at}: id is used twice`);
    seen.add(m?.id);
    if (!Array.isArray(m?.lenses) || m.lenses.length === 0 || m.lenses.length > MAX_LENSES) out.push(`${at}: \`lenses\` names 1 to ${MAX_LENSES} lenses`);
    else for (const l of m.lenses) if (!LENSES.includes(l)) out.push(`${at}: unknown lens "${l}" (known: ${LENSES.join(', ')})`);
    if (!Array.isArray(m?.surfaces) || m.surfaces.length === 0) out.push(`${at}: \`surfaces\` lists the repo paths the mission exercises`);
  }
  return out;
}

/**
 * The runs a body of text records, oldest first.
 * @param {string[]} texts the weekly epics' comment bodies
 * @returns {Run[]}
 */
export function parseRunLog(texts) {
  /** @type {Run[]} */
  const runs = [];
  for (const t of texts) {
    for (const m of t.matchAll(RUN_LINE)) runs.push({ date: m[1], mission: m[2], lenses: m[3] ? m[3].split(',') : [] });
  }
  return runs.sort((a, b) => a.date.localeCompare(b.date) || a.mission.localeCompare(b.mission));
}

/** @param {Run} r */
export const runLine = (r) => `explorer-run: ${r.date} mission=${r.mission} lenses=${r.lenses.join(',')}`;

// A mission that ran within this many days cannot win on changed code alone,
// so a mission over a busy part of the tree takes at most one day in this many
// and the rest still rotate.
export const MISSION_COOLDOWN_DAYS = 3;

/**
 * Today's mission and lenses. A mission whose surfaces changed since its own
 * last run comes first (fresh code is where fresh defects are), the most
 * changed first, unless it ran within MISSION_COOLDOWN_DAYS; else the mission
 * run least recently (never run counts as oldest); ties go to the order of the
 * missions file. A mission never run counts its changes from the first run on
 * record. The lenses are the mission's own, then any lens unused for
 * LENS_BACKSTOP_DAYS (also counted from the first run on record) takes the
 * last slot, so the run still brings at most MAX_LENSES.
 * @param {Mission[]} missions
 * @param {Run[]} runs
 * @param {{ date: string, path: string }[]} changes paths changed on main, with the day each change landed
 * @param {string} today YYYY-MM-DD
 */
export function pickMission(missions, runs, changes, today) {
  const last = new Map();
  for (const r of runs) last.set(r.mission, r.date);
  const first = runs[0]?.date;
  const hits = (/** @type {Mission} */ m) => {
    const since = last.get(m.id) ?? first;
    if (!since) return 0;
    return changes.filter((c) => (last.has(m.id) ? c.date > since : c.date >= since) && m.surfaces.some((s) => c.path.startsWith(s))).length;
  };
  const ranked = missions
    .map((m, i) => {
      const l = last.get(m.id) ?? '';
      const cooling = l !== '' && day(today) - day(l) < MISSION_COOLDOWN_DAYS;
      return { m, i, hits: cooling ? 0 : hits(m), last: l };
    })
    .sort((a, b) => (b.hits > 0 ? 1 : 0) - (a.hits > 0 ? 1 : 0) || b.hits - a.hits || a.last.localeCompare(b.last) || a.i - b.i);
  const mission = ranked[0].m;
  const why = ranked[0].hits > 0 ? `${ranked[0].hits} change(s) under its surfaces since it last ran` : ranked[0].last ? `least recently run (${ranked[0].last})` : 'never run';

  const lensLast = new Map();
  for (const r of runs) for (const l of r.lenses) lensLast.set(l, r.date);
  // A lens never used counts from the first run on record, so the program's
  // first week runs the missions' own lenses rather than the backstop's.
  const since = first ?? today;
  const stale = LENSES.filter((l) => !mission.lenses.includes(l) && day(today) - day(lensLast.get(l) ?? since) >= LENS_BACKSTOP_DAYS);
  const lenses = [...mission.lenses];
  if (stale.length > 0) {
    if (lenses.length >= MAX_LENSES) lenses[MAX_LENSES - 1] = stale[0];
    else lenses.push(stale[0]);
  }
  return { mission, lenses, why };
}

/**
 * Check one lens finding against the shape every lens returns. A malformed
 * finding is refused, never repaired: a lens that cannot fill the shape has
 * not found something it can defend.
 * @param {any} f
 * @param {string[]} [labels] the repo's labels, when known, to check `area` against
 * @returns {string[]}
 */
export function findingProblems(f, labels) {
  const out = [];
  if (!f || typeof f !== 'object') return ['finding: not an object'];
  if (!LENSES.includes(f.lens) && f.lens !== EXPLORER) out.push(`lens: "${f.lens}" is not a lens (or "${EXPLORER}" for the run's own findings)`);
  if (!['bug', 'suggestion'].includes(f.kind)) out.push('kind: bug or suggestion');
  if (!['high', 'medium', 'low'].includes(f.severity)) out.push('severity: high, medium or low');
  if (typeof f.key !== 'string' || !KEY.test(f.key)) out.push('key: 3 to 120 chars of lowercase letters, digits and : . _ / -');
  if (typeof f.area !== 'string' || !/^area:[a-z-]+$/.test(f.area)) out.push('area: the area:<subsystem> label the issue carries');
  else if (labels && !labels.includes(f.area)) out.push(`area: "${f.area}" is not a label in the repo (known: ${labels.filter((l) => l.startsWith('area:')).join(', ')})`);
  for (const field of ['surface', 'claim', 'evidence', 'failure_scenario']) {
    if (typeof f[field] !== 'string' || f[field].trim().length < 3) out.push(`${field}: required`);
  }
  // A digest finding's repro is the probe itself (make probe), which the
  // issue names; every other bug carries its own.
  if (f.kind === 'bug' && !String(f.key).startsWith('digest:') && (typeof f.repro !== 'string' || f.repro.trim() === '')) {
    out.push('repro: a bug carries its repro, the full text of a walkthrough script or a CLI/curl script and its failing output');
  }
  const allowed = new Set(['lens', 'kind', 'severity', 'key', 'area', 'surface', 'claim', 'evidence', 'failure_scenario', 'repro']);
  for (const k of Object.keys(f)) if (!allowed.has(k)) out.push(`${k}: not a finding field`);
  return out;
}

/** @param {string} body */
export function issueKey(body) {
  return body.match(KEY_LINE)?.[1] ?? null;
}

/** @param {string} key */
export const keyLine = (key) => `explorer-key: ${key}`;

/**
 * The Monday of the week a date falls in, and the weekly epic's title.
 * @param {string} date YYYY-MM-DD
 */
export function weekOf(date) {
  if (!DATE.test(date)) throw new Error(`weekOf: "${date}" is not YYYY-MM-DD`);
  const d = new Date(`${date}T00:00:00Z`);
  const back = (d.getUTCDay() + 6) % 7;
  d.setUTCDate(d.getUTCDate() - back);
  const monday = d.toISOString().slice(0, 10);
  return { monday, title: `explorer: week of ${monday}` };
}

const SEV = { high: 0, medium: 1, low: 2 };

/**
 * What filing today's findings means, as actions for explorer.mjs to apply:
 * a finding whose key an open issue already carries is a dated sighting on
 * that issue, never a second issue; a new bug is its own Bug issue, filed at
 * once; a new suggestion is a Task under the week's epic, until the week's
 * cap, most severe first; anything over the cap is reported, not filed. Two
 * findings with one key in the same run file once.
 * @param {Finding[]} findings already validated
 * @param {Issue[]} open open issues carrying an explorer key
 * @param {number} suggestionsThisWeek suggestions the week's epic already holds
 * @param {{ date: string, mission: string }} run
 */
export function planFiling(findings, open, suggestionsThisWeek, run) {
  const byKey = new Map();
  const filedOn = new Map();
  for (const i of open) {
    const k = issueKey(i.body);
    if (k && !byKey.has(k)) byKey.set(k, i.number);
    if (i.createdAt) filedOn.set(i.number, i.createdAt.slice(0, 10));
  }
  /** @type {any[]} */
  const actions = [];
  const done = new Set();
  let room = Math.max(0, WEEKLY_SUGGESTION_CAP - suggestionsThisWeek);
  const ordered = [...findings].sort((a, b) => SEV[a.severity] - SEV[b.severity] || a.key.localeCompare(b.key));
  for (const f of ordered) {
    if (done.has(f.key)) continue;
    done.add(f.key);
    const existing = byKey.get(f.key);
    if (existing && filedOn.get(existing) === run.date) {
      // Filed by an earlier attempt of this same run (a re-run after a
      // failure): already counted, not a second sighting.
      actions.push({ action: 'filed-today', issue: existing, key: f.key });
    } else if (existing) {
      actions.push({ action: 'sighting', issue: existing, key: f.key, body: `Seen again ${run.date} in mission \`${run.mission}\` (${f.lens} lens, ${f.surface}): ${f.claim}` });
    } else if (f.kind === 'bug') {
      actions.push({ action: 'bug', key: f.key, area: f.area, title: issueTitle(f), body: issueBody(f, run) });
    } else if (room > 0) {
      room--;
      actions.push({ action: 'suggestion', key: f.key, area: f.area, title: issueTitle(f), body: issueBody(f, run) });
    } else {
      actions.push({ action: 'over-cap', key: f.key, title: issueTitle(f) });
    }
  }
  return actions;
}

/** @param {Finding} f */
function issueTitle(f) {
  const claim = f.claim.replace(/\s+/g, ' ').trim().replace(/\.$/, '');
  const t = `${f.kind === 'bug' ? 'fix' : 'feat'}: ${claim.charAt(0).toLowerCase()}${claim.slice(1)}`;
  return t.length > 100 ? t.slice(0, 99) + '…' : t;
}

/** @param {Finding} f @param {{ date: string, mission: string }} run */
function issueBody(f, run) {
  const parts = [
    `**Claim:** ${f.claim}`,
    `**Surface:** ${f.surface}`,
    `**Failure scenario:** ${f.failure_scenario}`,
    `**Evidence:** ${f.evidence}`,
  ];
  if (f.repro) parts.push(`**Repro:**\n\n\`\`\`\n${f.repro.trim()}\n\`\`\``);
  parts.push(`Found by the synthetic user (#961): mission \`${run.mission}\`, ${run.date}, ${f.lens} lens, severity ${f.severity}.`);
  parts.push(keyLine(f.key));
  return parts.join('\n\n');
}

/**
 * The weekly roll-over, when a new week's epic opens: last week's epic
 * closes, and each of its sub-issues still open is either carried into the
 * new epic (it was seen again during the week, so it is still real) or closed
 * as not planned (nobody promoted it and nothing saw it again). A sub-issue
 * the architect promoted is no longer a Task, and is left alone wherever it
 * now lives.
 * @param {{ number: number, type: string, state: string, sightings: string[] }[]} subIssues
 *   sightings: the dates of its "Seen again" comments
 * @param {string} weekStart the old epic's Monday
 */
export function planRollover(subIssues, weekStart) {
  return subIssues
    .filter((s) => s.state === 'OPEN' && s.type === 'Task')
    .map((s) => (s.sightings.some((d) => d >= weekStart) ? { action: 'carry', issue: s.number } : { action: 'close', issue: s.number }));
}

/** @param {string} body */
export const sightingDate = (body) => body.match(/^Seen again (\d{4}-\d{2}-\d{2}) /)?.[1] ?? null;

/**
 * The key a digest finding files under, whichever lens reports it, so the
 * same digest finding seen by two lenses or on two days is one issue.
 * @param {string} kind a digest finding's kind
 * @param {string} key a digest finding's key
 */
export function digestKey(kind, key) {
  const slug = `${kind}:${key}`
    .toLowerCase()
    .replace(/[^a-z0-9:._/-]+/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-|-$/g, '');
  return `digest:${slug}`.slice(0, 120);
}

/**
 * An issue body carrying the key, for adopting an issue a person filed: the
 * key line is appended once, and an issue already keyed is refused rather
 * than given a second identity.
 * @param {string} body
 * @param {string} key
 */
export function adoptBody(body, key) {
  const existing = issueKey(body);
  if (existing) throw new Error(`already carries ${keyLine(existing)}`);
  if (!KEY.test(key)) throw new Error(`"${key}" is not a finding key`);
  return `${body.trimEnd()}\n\n${keyLine(key)}`;
}
