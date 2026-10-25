import { describe, it, expect } from "vitest";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { load } from "js-yaml";
import { CLIP_END, CLIP_START, PENDING_SHA } from "../e2e/walkthroughpr.mjs";

// The run:demo workflow's invariants (#1180), read from the workflow itself,
// so a change to it that breaks one fails make test-web rather than a PR's
// demo run: the token boundary (a job that holds a write token checks nothing
// out and runs no file from the tree, and a job that runs the tree's code
// holds no write token), the matrix fan-in, and the splice the publish job
// runs to write the clip into the PR body, played here against fixtures
// exactly as the job plays it.
type Step = { name?: string; uses?: string; run?: string; env?: Record<string, string>; with?: Record<string, unknown> };
type Job = { permissions?: Record<string, string>; steps: Step[]; needs?: string | string[]; if?: string; strategy?: { "fail-fast"?: boolean; matrix?: unknown } };
type Workflow = { on: Record<string, unknown>; permissions: Record<string, string>; concurrency?: unknown; jobs: Record<string, Job> };

const read = (f: string) => load(readFileSync(join(__dirname, "../../.github/workflows", f), "utf8")) as Workflow;
const demo = read("demo.yml");
const writes = (j: Job) => Object.values(j.permissions ?? {}).includes("write");
const checksOut = (j: Job) => j.steps.some((s) => s.uses?.startsWith("actions/checkout"));
// A command line that would run a file from the tree or install its
// dependencies (whose install scripts are the tree's code): anything but an
// inline node -e program, which is the workflow's own text.
const RUNS_TREE = /^\s*(?:node(?!\s+-e\s)|bash|sh|npm|npx|pnpm|make|source|\.)(?:\s|$)/m;

describe("the run:demo workflow's token boundary", () => {
  it("defaults every job to a read-only token", () => {
    expect(demo.permissions).toEqual({ contents: "read" });
  });

  it("gives a write token only to the jobs that publish to the PR and report on main", () => {
    expect(Object.keys(demo.jobs).filter((k) => writes(demo.jobs[k])).sort()).toEqual(["publish", "report"]);
  });

  it("never checks the tree out, or runs a file from it, in a job that holds a write token", () => {
    for (const [name, job] of Object.entries(demo.jobs).filter(([, j]) => writes(j))) {
      expect(checksOut(job), `${name} checks out the tree`).toBe(false);
      for (const s of job.steps) {
        if (s.run) expect(s.run, `${name}: ${s.name}`).not.toMatch(RUNS_TREE);
      }
    }
  });

  it("holds only a read token in every job that checks the tree out", () => {
    for (const [name, job] of Object.entries(demo.jobs).filter(([, j]) => checksOut(j))) {
      expect(writes(job), `${name} checks out the tree and holds a write token`).toBe(false);
    }
  });

  it("checks out without persisting the token into the tree's git config", () => {
    for (const job of Object.values(demo.jobs)) {
      for (const s of job.steps.filter((s) => s.uses?.startsWith("actions/checkout"))) {
        expect(s.with?.["persist-credentials"]).toBe(false);
      }
    }
  });

  it("passes event text to a shell only through env, never interpolated into a script", () => {
    for (const job of Object.values(demo.jobs)) {
      for (const s of job.steps) {
        if (s.run) expect(s.run).not.toMatch(/\$\{\{\s*(?:github\.event|matrix|needs|steps)\./);
      }
    }
  });
});

describe("the run:demo workflow's matrix", () => {
  it("records each selected demo on a runner of its own, every one to the end even when another fails", () => {
    const record = demo.jobs.record;
    expect(record.needs).toBe("select");
    expect(record.strategy?.["fail-fast"]).toBe(false);
    expect(JSON.stringify(record.strategy?.matrix)).toContain("fromJSON(needs.select.outputs.matrix)");
  });

  it("combines every leg into one comment before anything is published, and publishes only from the combined result", () => {
    expect([demo.jobs.combine.needs].flat()).toEqual(["select", "record"]);
    expect(demo.jobs.publish.needs).toBe("combine");
    expect(demo.jobs.report.needs).toBe("combine");
  });

  it("runs on the demo label for a PR, and on a schedule (or by hand) for the freshness run on main", () => {
    expect(demo.on.pull_request).toEqual({ types: ["labeled"] });
    expect(demo.on.schedule).toBeDefined();
    expect(demo.on).toHaveProperty("workflow_dispatch");
    expect(demo.jobs.publish.if).toContain("pull_request");
    expect(demo.jobs.report.if).toContain("refs/heads/main");
  });

  it("swaps the pending SHA the staged clip and comment are written against for the evidence commit's", () => {
    const run = demo.jobs.publish.steps.map((s) => s.run ?? "").join("\n");
    expect(run).toContain(PENDING_SHA);
  });
});

describe("the demo tag lint", () => {
  const lint = read("demo-tag.yml");

  it("runs on every PR as its body or head changes, with a read-only token", () => {
    expect(lint.on.pull_request).toEqual({ types: ["opened", "edited", "synchronize", "reopened"] });
    expect(lint.permissions).toEqual({ contents: "read" });
    for (const job of Object.values(lint.jobs)) expect(writes(job)).toBe(false);
  });
});

// The splice the publish job runs (node -e "$SPLICE", inline in the workflow
// so the job runs nothing from the tree), played against fixtures.
describe("the publish job's splice of the clip into the PR body", () => {
  const step = demo.jobs.publish.steps.find((s) => s.env?.SPLICE);
  const dir = mkdtempSync(join(tmpdir(), "og-splice-"));
  const splice = (body: string, clip: string): { ok: boolean; out: string; err: string } => {
    writeFileSync(join(dir, "body.md"), body);
    writeFileSync(join(dir, "clip.md"), clip);
    try {
      execFileSync(process.execPath, ["-e", step!.env!.SPLICE], {
        env: { BODY_FILE: join(dir, "body.md"), CLIP_FILE: join(dir, "clip.md"), OUT_FILE: join(dir, "out.md") },
        stdio: ["ignore", "pipe", "pipe"],
      });
      return { ok: true, out: readFileSync(join(dir, "out.md"), "utf8"), err: "" };
    } catch (e) {
      return { ok: false, out: "", err: String((e as { stderr?: Buffer }).stderr ?? e) };
    }
  };
  const block = (clip: string, eol = "\n") => [CLIP_START, ...clip.split("\n"), CLIP_END].join(eol);

  it("is a step of the publish job", () => {
    expect(step).toBeDefined();
  });

  it("replaces the block between the markers in place, leaving every byte outside it alone", () => {
    const before = "## Outcome\n\nx\n\n## Demo\n\ndemo: t\n\n";
    const after = "\n\n## Docs\n\ny\n";
    const r = splice(`${before}${block("old clip")}${after}`, "new clip\n");
    expect(r.ok).toBe(true);
    expect(r.out).toBe(`${before}${block("new clip")}${after}`);
  });

  it("inserts the block after the demo tag line when the body has no block yet", () => {
    const r = splice("## Demo\n\ndemo: t u\n\n## Docs\n", "clip");
    expect(r.out).toBe(`## Demo\n\ndemo: t u\n\n${block("clip")}\n\n## Docs\n`);
  });

  it("skips a tag quoted in a code fence or an HTML comment when choosing where to insert", () => {
    const body = "<!--\ndemo: x\n-->\n```\ndemo: y\n```\n- demo: t\nrest";
    expect(splice(body, "clip").out).toBe(`<!--\ndemo: x\n-->\n\`\`\`\ndemo: y\n\`\`\`\n- demo: t\n\n${block("clip")}\n\nrest`);
  });

  it("appends the block when the body carries no tag (a run that recorded only a demo the PR changed)", () => {
    expect(splice("Some body.", "clip").out).toBe(`Some body.\n\n${block("clip")}`);
    expect(splice("", "clip").out).toBe(block("clip"));
  });

  it("keeps a body's CRLF line endings, as the web editor writes them", () => {
    const r = splice("## Demo\r\n\r\ndemo: t\r\n\r\n## Docs\r\n", "line one\nline two");
    expect(r.out).toBe(`## Demo\r\n\r\ndemo: t\r\n\r\n${block("line one\nline two", "\r\n")}\r\n\r\n## Docs\r\n`);
  });

  it("is idempotent: splicing the same clip again changes nothing", () => {
    const once = splice("## Demo\n\ndemo: t\n\nafter\n", "clip").out;
    expect(splice(once, "clip").out).toBe(once);
  });

  it("refuses a staged clip that carries a marker, which could move the block's edge", () => {
    for (const clip of [`a\n${CLIP_END}\nb`, `${CLIP_START}`]) {
      const r = splice("demo: t", clip);
      expect(r.ok).toBe(false);
      expect(r.err).toContain("marker");
    }
  });

  it("refuses to guess at a body whose markers are unpaired, doubled or reversed", () => {
    for (const body of [`${CLIP_START}\nx`, `x\n${CLIP_END}`, `${block("a")}\n${block("b")}`, `${CLIP_END}\n${CLIP_START}`]) {
      const r = splice(body, "clip");
      expect(r.ok, body).toBe(false);
      expect(r.err).toContain("marker");
    }
  });
});
