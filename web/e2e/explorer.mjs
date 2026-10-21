// The synthetic user's command (#966): the deterministic half of an /explore
// run. Decisions come from explorerlib.mjs (pure, tested); this file only
// reads their inputs (the missions file, git, GitHub through an authenticated
// gh) and applies their outputs.
//
//   node web/e2e/explorer.mjs pick [--today YYYY-MM-DD]
//       today's mission and lenses, as JSON on stdout
//   node web/e2e/explorer.mjs validate <findings.json>
//       check a run's findings (a JSON array) against the finding shape;
//       exits 1 naming every problem
//   node web/e2e/explorer.mjs file <findings.json> --mission <id> --lenses <a,b> --report <report.md> [--confirm-new k1,k2] [--today D] [--dry-run]
//       file the findings (bugs at once, suggestions under the week's epic,
//       sightings on what is already open), open the week's epic first if it
//       does not exist yet (rolling last week's over), and post the day's
//       report with its run marker on the epic. A finding that looks like an
//       open issue is held, not filed: adopt that issue, or pass its key to
//       --confirm-new once you have read both. --dry-run prints the plan and
//       touches nothing.
//   node web/e2e/explorer.mjs digest-keys <digest.json>
//       each digest finding with the key it files under
//   node web/e2e/explorer.mjs adopt <issue> <key> [--dry-run]
//       give an open issue a person filed the key of the finding it covers,
//       so the next sighting lands on it instead of filing a duplicate
import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { load } from 'js-yaml';
import {
  adoptBody,
  digestKey,
  findingProblems,
  missionProblems,
  parseRunLog,
  pickMission,
  planFiling,
  planRollover,
  runLine,
  sightingDate,
  weekOf,
} from './explorerlib.mjs';

const REPO = process.env.OG_EXPLORER_REPO ?? 'hyperscaleav/omniglass';
const MISSIONS = new URL('./explorer/missions.yaml', import.meta.url);
// How far back the run log is read: enough weekly epics to know when every
// mission and lens last ran.
const LOG_WEEKS = 8;

const args = process.argv.slice(2);
const cmd = args.shift();
const flag = (name) => {
  const i = args.indexOf(`--${name}`);
  return i >= 0 ? args[i + 1] : undefined;
};
const today = flag('today') ?? new Date().toISOString().slice(0, 10);
const dryRun = args.includes('--dry-run');

const run = (bin, a, input) => execFileSync(bin, a, { encoding: 'utf8', input, stdio: [input ? 'pipe' : 'ignore', 'pipe', 'inherit'] });
const gh = (...a) => run('gh', a);
const ghJSON = (...a) => JSON.parse(gh(...a));
const graphql = (query, vars = {}) => {
  const a = ['api', 'graphql', '-f', `query=${query}`];
  for (const [k, v] of Object.entries(vars)) a.push(typeof v === 'number' ? '-F' : '-f', `${k}=${v}`);
  return JSON.parse(run('gh', a)).data;
};

function missions() {
  const doc = load(readFileSync(MISSIONS, 'utf8'));
  const problems = missionProblems(doc);
  if (problems.length) {
    for (const p of problems) console.error(p);
    process.exit(1);
  }
  return doc.missions;
}

// The weekly epics, newest first, by their title.
function weeklyEpics(state = 'all') {
  return ghJSON('issue', 'list', '--repo', REPO, '--state', state, '--search', 'in:title "explorer: week of" sort:created-desc', '--limit', String(LOG_WEEKS), '--json', 'number,title,state')
    .filter((i) => /^explorer: week of \d{4}-\d{2}-\d{2}$/.test(i.title))
    .sort((a, b) => b.title.localeCompare(a.title));
}

function runLog() {
  const bodies = [];
  for (const e of weeklyEpics()) bodies.push(...ghJSON('issue', 'view', String(e.number), '--repo', REPO, '--json', 'comments').comments.map((c) => c.body));
  return parseRunLog(bodies);
}

// The repo's labels, so a finding's area is checked against the labels that
// exist before anything is filed, not discovered missing halfway through.
const repoLabels = () => ghJSON('label', 'list', '--repo', REPO, '--limit', '200', '--json', 'name').map((l) => l.name);

function readFindings(path) {
  const findings = JSON.parse(readFileSync(path, 'utf8'));
  if (!Array.isArray(findings)) throw new Error(`${path}: a JSON array of findings`);
  const labels = repoLabels();
  const problems = findings.flatMap((f, i) => findingProblems(f, labels).map((p) => `findings[${i}]${f?.key ? ` (${f.key})` : ''}: ${p}`));
  return { findings, problems };
}

function pick() {
  const ms = missions();
  const runs = runLog();
  // Every change on main since the first run on record, with the day it
  // landed, so each mission counts only what changed since its own last run.
  const changes = [];
  if (runs.length) {
    let date = '';
    for (const line of run('git', ['log', `--since=${runs[0].date}T00:00:00Z`, '--name-only', '--format=@%cs', 'origin/main']).split('\n')) {
      if (line.startsWith('@')) date = line.slice(1);
      else if (line) changes.push({ date, path: line });
    }
  }
  const { mission, lenses, why } = pickMission(ms, runs, changes, today);
  console.log(JSON.stringify({ today, mission, lenses, why }, null, 2));
}

function validate(path) {
  const { findings, problems } = readFindings(path);
  for (const p of problems) console.error(p);
  console.log(`${findings.length} findings, ${problems.length} problems`);
  process.exit(problems.length ? 1 : 0);
}

const issueNodeID = (n) => graphql(`query($o:String!,$r:String!,$n:Int!){repository(owner:$o,name:$r){issue(number:$n){id}}}`, { o: REPO.split('/')[0], r: REPO.split('/')[1], n }).repository.issue.id;

function subIssues(n) {
  const d = graphql(
    `query($o:String!,$r:String!,$n:Int!){repository(owner:$o,name:$r){issue(number:$n){subIssues(first:100){nodes{number state issueType{name} comments(last:50){nodes{body}}}}}}}`,
    { o: REPO.split('/')[0], r: REPO.split('/')[1], n },
  );
  return d.repository.issue.subIssues.nodes.map((s) => ({
    number: s.number,
    state: s.state,
    type: s.issueType?.name ?? '',
    sightings: s.comments.nodes.map((c) => sightingDate(c.body)).filter(Boolean),
  }));
}

function addSubIssue(parent, child) {
  graphql(`mutation($p:ID!,$c:ID!){addSubIssue(input:{issueId:$p,subIssueId:$c}){issue{number}}}`, { p: issueNodeID(parent), c: issueNodeID(child) });
}

// A suggestion filed but not linked would sit outside every epic, never
// counted or rolled over; one retry, then it is named in the report.
function linkWithRetry(epic, n, errors) {
  for (let attempt = 0; attempt < 2; attempt++) {
    try {
      addSubIssue(epic, n);
      return;
    } catch (e) {
      if (attempt === 1) errors.push(`#${n} filed but not linked under #${epic}: ${e.message.split('\n')[0]}`);
    }
  }
}

function removeSubIssue(parent, child) {
  graphql(`mutation($p:ID!,$c:ID!){removeSubIssue(input:{issueId:$p,subIssueId:$c}){issue{number}}}`, { p: issueNodeID(parent), c: issueNodeID(child) });
}

// The week's epic, opened (and last week's rolled over into it) when absent.
function weekEpic(plan) {
  const { monday, title } = weekOf(today);
  const open = weeklyEpics('open');
  const current = open.find((e) => e.title === title);
  if (current) return current.number;
  const body = `The synthetic user's findings for the week of ${monday} (#961): suggestions are this epic's sub-issues, bugs are linked from the daily reports below. Weekly triage: promote a suggestion by retyping it to Feature (or turning it into a /define-work definition); anything neither promoted nor seen again closes when next week's epic opens.`;
  plan.push({ action: 'open-epic', title });
  if (dryRun) return 0;
  const url = gh('issue', 'create', '--repo', REPO, '--type', 'Epic', '--title', title, '--body', body).trim();
  const epic = Number(url.split('/').pop());
  for (const old of open.filter((e) => e.title < title)) {
    const oldMonday = old.title.slice(-10);
    for (const a of planRollover(subIssues(old.number), oldMonday)) {
      if (a.action === 'carry') {
        removeSubIssue(old.number, a.issue);
        addSubIssue(epic, a.issue);
        gh('issue', 'comment', String(a.issue), '--repo', REPO, '--body', `Carried into #${epic}: seen again during the week of ${oldMonday}.`);
      } else {
        gh('issue', 'close', String(a.issue), '--repo', REPO, '--reason', 'not planned', '--comment', `Closed with the week of ${oldMonday}: not promoted, and not seen again that week. A later sighting files it afresh.`);
      }
    }
    gh('issue', 'close', String(old.number), '--repo', REPO, '--comment', `Week closed; carried items now live under #${epic}.`);
  }
  return epic;
}

function file(path) {
  const missionId = flag('mission');
  const report = flag('report');
  const lenses = (flag('lenses') ?? '').split(',').filter(Boolean);
  if (!missionId || !report || lenses.length === 0) throw new Error('file: --mission <id>, --lenses <a,b> (the ones pick chose) and --report <report.md> are required');
  const m = missions().find((x) => x.id === missionId);
  if (!m) throw new Error(`file: no mission "${missionId}"`);
  const { findings, problems } = readFindings(path);
  if (problems.length) {
    for (const p of problems) console.error(p);
    process.exit(1);
  }
  const plan = [];
  const epic = weekEpic(plan);
  // Every open issue, keyed or not: keyed ones take sightings, and any of
  // them can be the look-alike a new finding is held against.
  const open = ghJSON('issue', 'list', '--repo', REPO, '--state', 'open', '--limit', '1000', '--json', 'number,title,body,createdAt');
  const already = epic ? subIssues(epic).filter((s) => s.type === 'Task').length : 0;
  const confirmed = (flag('confirm-new') ?? '').split(',').filter(Boolean);
  const actions = planFiling(findings, open, already, { date: today, mission: missionId }, { confirmed });
  plan.push(...actions);
  const filed = [];
  if (!dryRun) {
    // Each action stands alone: one that fails is reported and the rest still
    // go, and the report and its run marker are posted whatever happened, so
    // tomorrow's pick knows today ran. A re-run after a failure finds what
    // this run already filed by its key (planFiling's filed-today).
    const errors = [];
    for (const a of actions) {
      try {
        if (a.action === 'sighting') {
          // One sighting a day per issue, so a re-run after a failure does
          // not count the same day twice.
          const seen = ghJSON('issue', 'view', String(a.issue), '--repo', REPO, '--json', 'comments').comments.some((c) => sightingDate(c.body) === today);
          if (!seen) gh('issue', 'comment', String(a.issue), '--repo', REPO, '--body', a.body);
          filed.push(`- #${a.issue} seen again (\`${a.key}\`)`);
        } else if (a.action === 'bug' || a.action === 'suggestion') {
          const url = gh('issue', 'create', '--repo', REPO, '--type', a.action === 'bug' ? 'Bug' : 'Task', '--label', a.area, '--title', a.title, '--body', a.body).trim();
          const n = Number(url.split('/').pop());
          filed.push(`- #${n} ${a.action} (\`${a.key}\`)`);
          if (a.action === 'suggestion') linkWithRetry(epic, n, errors);
        } else if (a.action === 'over-cap') {
          filed.push(`- over the weekly cap, not filed: ${a.title} (\`${a.key}\`)`);
        } else if (a.action === 'possible-duplicate') {
          filed.push(`- held as a possible duplicate of ${a.candidates.map((c) => `#${c.number}`).join(', ')}, not filed: ${a.title} (\`${a.key}\`)`);
        } else if (a.action === 'same-run-duplicate') {
          filed.push(`- folded into "${a.of}" from this run: ${a.title} (\`${a.key}\`)`);
        }
      } catch (e) {
        errors.push(`\`${a.key}\` (${a.action}): ${e.message.split('\n')[0]}`);
      }
    }
    const body = [
      readFileSync(report, 'utf8').trim(),
      '',
      '**Filed**',
      '',
      ...(filed.length ? filed : ['- nothing']),
      ...(errors.length ? ['', '**Failed to file** (a re-run picks these up)', '', ...errors.map((e) => `- ${e}`)] : []),
      '',
      runLine({ date: today, mission: missionId, lenses }),
    ].join('\n');
    gh('issue', 'comment', String(epic), '--repo', REPO, '--body', body);
    if (errors.length) {
      console.error(errors.join('\n'));
      process.exitCode = 1;
    }
  }
  console.log(JSON.stringify({ dryRun, epic, plan }, null, 2));
}

function digestKeys(path) {
  const d = JSON.parse(readFileSync(path, 'utf8'));
  for (const f of d.findings) console.log(`${digestKey(f.kind, f.key)}\t${f.severity}\t${f.detail}`);
}

function adopt(issue, key) {
  const { body } = ghJSON('issue', 'view', issue, '--repo', REPO, '--json', 'body');
  const next = adoptBody(body, key);
  if (!dryRun) run('gh', ['issue', 'edit', issue, '--repo', REPO, '--body-file', '-'], next);
  console.log(dryRun ? next : `#${issue} now carries explorer-key: ${key}`);
}

try {
  if (cmd === 'pick') pick();
  else if (cmd === 'digest-keys' && args[0]) digestKeys(args[0]);
  else if (cmd === 'adopt' && args[0] && args[1]) adopt(args[0], args[1]);
  else if (cmd === 'validate' && args[0]) validate(args[0]);
  else if (cmd === 'file' && args[0]) file(args[0]);
  else {
    console.error('usage: explorer.mjs pick [--today D] | digest-keys <digest.json> | adopt <issue> <key> [--dry-run] | validate <findings.json> | file <findings.json> --mission <id> --lenses <a,b> --report <report.md> [--confirm-new k1,k2] [--today D] [--dry-run]');
    process.exit(2);
  }
} catch (e) {
  console.error(`explorer: ${e.message}`);
  process.exit(1);
}
