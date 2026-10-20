import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";

// Guard (#953): no JSX prop calls a hook. Solid compiles a prop expression to a
// getter, so `row={useXRow(id)}` calls useXRow on every read of the prop, not
// once: each read mints a fresh query observer, and a stale observer refetches
// on mount. Read inside a memo that the query's own data re-runs, that was a
// request storm (about 1,300 /event-rules requests in 6 seconds with one rule
// blade open). Call the hook once in the component body and pass the variable:
//
//   const row = useXRow(p.id);
//   return <BladeTitle row={row} fallback={p.id} />;
const HOOK_IN_PROP = /\b[\w:-]+=\{\s*use[A-Z]\w*\s*\(/g;

function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p, out);
    else if (/\.tsx$/.test(name) && !/\.test\.tsx$/.test(name)) out.push(p);
  }
  return out;
}

// Comments may quote the forbidden form to explain it; only code counts.
function stripComments(src: string): string {
  return src.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/.*$/gm, "$1");
}

function hookInPropHits(src: string): string[] {
  return [...stripComments(src).matchAll(HOOK_IN_PROP)].map((m) => m[0]);
}

describe("no JSX prop calls a hook", () => {
  it("flags the inline form and passes the variable form", () => {
    expect(hookInPropHits("<BladeTitle row={useRuleRow(p.id)} fallback={p.id} />")).toHaveLength(1);
    expect(hookInPropHits("<X items={ useThings() } />")).toHaveLength(1);
    expect(hookInPropHits("const row = useRuleRow(p.id);\nreturn <BladeTitle row={row} fallback={p.id} />;")).toEqual([]);
    expect(hookInPropHits("// written inline, `row={useRuleRow(id)}` stormed")).toEqual([]);
  });

  it("no source file passes a hook call as a JSX prop", () => {
    const hits: string[] = [];
    for (const f of walk(join(__dirname))) {
      for (const h of hookInPropHits(readFileSync(f, "utf8"))) hits.push(`${f.replace(__dirname + "/", "")}: ${h}`);
    }
    expect(hits).toEqual([]);
  });
});
