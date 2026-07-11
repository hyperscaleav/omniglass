export const meta = {
  name: 'profile-pictures-sdd',
  description: 'Subagent-driven build of the profile-pictures slice (9 TDD tasks, implement -> adversarial review -> fix, then whole-branch review)',
  phases: [
    { title: 'Task 1 avatar primitive' },
    { title: 'Task 2 storage columns' },
    { title: 'Task 3 gateway methods' },
    { title: 'Task 4 write API' },
    { title: 'Task 5 read API' },
    { title: 'Task 6 regenerate clients' },
    { title: 'Task 7 profile UI' },
    { title: 'Task 8 users admin UI' },
    { title: 'Task 9 docs + validation' },
    { title: 'Final whole-branch review' },
  ],
}

const WT = '/home/fred/code/hyperscaleav/omniglass/.claude/worktrees/feat+profile-pictures'
const PLAN = 'docs/superpowers/plans/2026-07-10-profile-pictures.md'

// meta is not a runtime binding in the script body; keep phase titles here too.
const PHASES = [
  'Task 1 avatar primitive',
  'Task 2 storage columns',
  'Task 3 gateway methods',
  'Task 4 write API',
  'Task 5 read API',
  'Task 6 regenerate clients',
  'Task 7 profile UI',
  'Task 8 users admin UI',
  'Task 9 docs + validation',
  'Final whole-branch review',
]

// Binding constraints copied from the plan's Global Constraints. Handed to every
// reviewer verbatim as its attention lens.
const CONSTRAINTS = `
- No em dashes anywhere (code comments, docs, commit subjects). No AI/assistant attribution.
- Commit subjects: conventional-commit type, lowercase first letter after the prefix.
- Migrations: run-once, never edited after applied, idempotent (add column if not exists), timestamp > 20260709160000.
- Every gateway method added in THREE places: storage.Gateway interface, *PG impl, UnimplementedGateway.
- API-first: after any route change, api/openapi.{json,yaml}, internal/cli/api_gen.go, web/src/api/schema.gen.ts are regenerated (make gen) and committed; no hand-editing generated files.
- Every admin route carries a.authn + a.require(resource, action); self routes carry a.authn only. No chi-native handler that bypasses the Huma authz middleware.
- loadPrincipal runs on EVERY authenticated request: it must select "avatar is not null" (a bool), never the avatar bytes.
- Image spec: accept JPEG/PNG/WebP (reject GIF and all else); reject payload > 8 MiB or any dimension > 8000px; center-crop square; 256x256; JPEG quality 82.
`.trim()

const IMPL_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['status', 'baseSha', 'headSha', 'testCommand', 'testResult', 'filesChanged', 'concerns'],
  properties: {
    status: { type: 'string', enum: ['DONE', 'DONE_WITH_CONCERNS', 'BLOCKED', 'NEEDS_CONTEXT'] },
    baseSha: { type: 'string', description: 'git rev-parse HEAD BEFORE any work this task' },
    headSha: { type: 'string', description: 'git rev-parse HEAD AFTER committing this task' },
    commits: { type: 'array', items: { type: 'string' } },
    testCommand: { type: 'string' },
    testResult: { type: 'string', description: 'PASS or FAIL plus a one-line summary (counts)' },
    filesChanged: { type: 'array', items: { type: 'string' } },
    concerns: { type: 'string', description: 'blockers, doubts, or "none"' },
  },
}

const REVIEW_SCHEMA = {
  type: 'object',
  additionalProperties: false,
  required: ['specCompliance', 'quality', 'findings', 'verdict', 'summary'],
  properties: {
    specCompliance: { type: 'string', enum: ['pass', 'fail'] },
    quality: { type: 'string', enum: ['approved', 'changes'] },
    findings: {
      type: 'array',
      items: {
        type: 'object',
        additionalProperties: false,
        required: ['severity', 'file', 'note'],
        properties: {
          severity: { type: 'string', enum: ['Critical', 'Important', 'Minor'] },
          file: { type: 'string' },
          note: { type: 'string' },
        },
      },
    },
    verdict: { type: 'string', enum: ['APPROVED', 'NEEDS_FIX'] },
    summary: { type: 'string' },
  },
}

function implPrompt(n) {
  return `You are an implementer subagent building ONE task of the omniglass profile-pictures slice, following strict TDD.

Working directory: ${WT}
Run: cd ${WT}   (you are on branch feat/profile-pictures; all work and commits happen here)

FIRST: run \`git rev-parse HEAD\` and record it as baseSha.

Then Read ${PLAN}, locate the section that starts with "### Task ${n}:", and implement ONLY that task. Follow its numbered steps EXACTLY: write the failing test first, run it and confirm it fails, implement the minimal code shown, run the test and confirm it passes, then commit with the exact message given. The task text contains the actual code and exact file paths; use them verbatim, adapting only where the step says to (e.g. reuse an existing test harness helper rather than inventing one).

Global constraints (bind this task):
${CONSTRAINTS}

Rules:
- Do not touch files outside this task's scope.
- Run every test command the task lists; the task is not DONE until they pass.
- Commit exactly as the task's commit step specifies (one or more commits).
- If the task depends on code from an earlier task, it is already committed on this branch; read it, do not recreate it.
- If you cannot proceed (missing context, a genuine plan error, a blocker), stop and report status BLOCKED or NEEDS_CONTEXT with the reason in concerns; do not guess.

AFTER committing: run \`git rev-parse HEAD\` and record it as headSha.

Return the structured result: status, baseSha, headSha, the commit shas you made, the test command you ran last, its PASS/FAIL result with counts, the files you changed, and any concerns ("none" if clean). Your structured output IS the result; do not address a human.`
}

function reviewPrompt(n, base, head) {
  return `You are a task reviewer for ONE task of the omniglass profile-pictures slice. Be adversarial: your job is to find where the implementation diverges from its spec or is poorly built, not to rubber-stamp.

Working directory: ${WT}  (cd there first)

Read the requirements: Read ${PLAN} and locate "### Task ${n}:". That section is the spec for this task, including the exact code, file paths, and test commands the implementer was told to produce.

Read the actual change: run \`git --no-pager log --oneline ${base}..${head}\` and \`git --no-pager diff ${base}..${head}\` to see exactly what was committed for this task.

Global constraints (the attention lens for this review):
${CONSTRAINTS}

Judge two things independently:
1. Spec compliance (pass/fail): does the diff implement what Task ${n} requires, nothing missing and nothing extra (no unrequested features/flags/endpoints)? A test that asserts nothing, or that does not actually exercise the behavior, is a spec failure. Verify the test would fail without the implementation.
2. Code quality (approved/changes): correctness bugs, doctrine violations (authz middleware, three-place gateway, loadPrincipal must not load bytes, em dashes, attribution, generated-file hand-edits), and real defects. Skip pure style nits.

If a requirement lives in unchanged code you cannot see in the diff, say so in a finding noted "cannot verify from diff"; do not fail spec on it alone.

Return structured: specCompliance, quality, a findings list (each severity Critical/Important/Minor + file + note), a verdict (APPROVED only if specCompliance=pass AND no Critical/Important findings, else NEEDS_FIX), and a one-line summary. Your structured output IS the result.`
}

function fixPrompt(n, findings) {
  return `You are a fix subagent for Task ${n} of the omniglass profile-pictures slice. A reviewer found blocking issues. Fix them, re-test, and commit.

Working directory: ${WT}  (cd there first). You are on branch feat/profile-pictures.

Context: Read ${PLAN} section "### Task ${n}:" for the task's intent and its test commands.

Blocking findings to resolve (Critical and Important only; you may also fix Minor if trivial):
${findings}

Global constraints (bind your fix):
${CONSTRAINTS}

FIRST run \`git rev-parse HEAD\` (baseSha). Fix ONLY these findings, staying within Task ${n}'s scope. Re-run the task's test command(s) and confirm they pass. Commit with a message like "fix: address review on <task ${n} subject>". Then run \`git rev-parse HEAD\` (headSha).

Return structured (same schema as an implementer): status (DONE if fixed and tests pass), baseSha, headSha, commits, testCommand, testResult with counts, filesChanged, and concerns. Your structured output IS the result.`
}

// ---- sequential per-task loop (shared worktree => no parallelism across tasks) ----
const ledger = []
let halted = false

for (let n = 1; n <= 9 && !halted; n++) {
  const phaseTitle = PHASES[n - 1]
  phase(phaseTitle)

  let impl = await agent(implPrompt(n), { label: `impl:task${n}`, phase: phaseTitle, schema: IMPL_SCHEMA })
  if (!impl) { ledger.push({ task: n, result: 'implementer died (null)' }); halted = true; break }
  if (impl.status === 'BLOCKED' || impl.status === 'NEEDS_CONTEXT') {
    ledger.push({ task: n, result: `HALT ${impl.status}`, concerns: impl.concerns, testResult: impl.testResult })
    halted = true
    break
  }
  if (/^FAIL/i.test(impl.testResult || '')) {
    ledger.push({ task: n, result: 'HALT tests failing after implement', testResult: impl.testResult, concerns: impl.concerns })
    halted = true
    break
  }

  // review -> fix loop (up to 2 fix rounds)
  let base = impl.baseSha, head = impl.headSha, verdict = 'NEEDS_FIX', lastReview = null
  for (let round = 0; round < 3; round++) {
    const review = await agent(reviewPrompt(n, base, head), { label: `review:task${n}:r${round}`, phase: phaseTitle, schema: REVIEW_SCHEMA })
    lastReview = review
    if (!review) { verdict = 'NEEDS_FIX'; break }
    verdict = review.verdict
    if (verdict === 'APPROVED') break
    const blocking = (review.findings || []).filter(f => f.severity === 'Critical' || f.severity === 'Important')
    if (blocking.length === 0) { verdict = 'APPROVED'; break } // only Minor -> pass, record minors
    if (round === 2) break // out of fix rounds
    const findingsText = blocking.map(f => `- [${f.severity}] ${f.file}: ${f.note}`).join('\n')
    const fix = await agent(fixPrompt(n, findingsText), { label: `fix:task${n}:r${round}`, phase: phaseTitle, schema: IMPL_SCHEMA })
    if (!fix || fix.status === 'BLOCKED') { verdict = 'NEEDS_FIX'; break }
    base = fix.baseSha; head = fix.headSha // re-review only the fix delta
  }

  const minors = (lastReview?.findings || []).filter(f => f.severity === 'Minor')
  ledger.push({
    task: n,
    result: verdict === 'APPROVED' ? 'complete' : 'INCOMPLETE (review not clean)',
    commits: impl.commits,
    testResult: impl.testResult,
    reviewSummary: lastReview?.summary,
    minors: minors.map(f => `${f.file}: ${f.note}`),
  })
  log(`Task ${n}: ${verdict === 'APPROVED' ? 'complete' : 'INCOMPLETE'} — ${lastReview?.summary || 'no review'}`)
  if (verdict !== 'APPROVED') { halted = true; break }
}

// ---- final whole-branch review (only if all 9 landed) ----
let finalReview = null
if (!halted) {
  phase(PHASES[9])
  finalReview = await agent(
    `You are the final whole-branch reviewer for the omniglass profile-pictures slice (issue #174).

Working directory: ${WT}  (cd there first).

Review the entire branch against main: run \`git --no-pager diff origin/main...HEAD\` (and \`git --no-pager log --oneline origin/main..HEAD\`). Also run the full gate yourself: \`make test\` and \`make gen && git status --porcelain\` (gen drift must be empty).

Design intent to hold it to: ${PLAN} and the spec docs/superpowers/specs/2026-07-10-profile-pictures-design.md. Key invariants: server-side authoritative image normalize (256x256 JPEG, bomb guards); base64 storage on the human row; loadPrincipal never loads avatar bytes; read side is a Huma JSON endpoint (not raw bytes) so authz middleware covers every route; one new permission principal:set-avatar with self needing none; all generated files committed and drift-free.

Global constraints:
${CONSTRAINTS}

Report: overall readiness (READY / NOT_READY to open the PR), the make test result, the gen-drift result, and any Critical/Important findings across the whole branch (file + note). Skip Minor style. Your structured output IS the result.`,
    { label: 'final-review', phase: PHASES[9], effort: 'high', schema: {
      type: 'object', additionalProperties: false,
      required: ['readiness', 'makeTest', 'genDrift', 'findings', 'summary'],
      properties: {
        readiness: { type: 'string', enum: ['READY', 'NOT_READY'] },
        makeTest: { type: 'string' },
        genDrift: { type: 'string' },
        findings: { type: 'array', items: { type: 'object', additionalProperties: false, required: ['severity', 'file', 'note'],
          properties: { severity: { type: 'string', enum: ['Critical', 'Important', 'Minor'] }, file: { type: 'string' }, note: { type: 'string' } } } },
        summary: { type: 'string' },
      },
    } },
  )
}

return { halted, ledger, finalReview }
