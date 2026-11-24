import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";

// Guard (#2115): an alarm's controls (Acknowledge, Snooze, the timeline toggle,
// Troubleshoot, Clear) are one AlarmActions group, so they share a height and
// a center line and never split across lines. Only the primitive may import
// AcknowledgeButton, SnoozeControl or the SnoozeButton default; every other
// file renders <AlarmActions>. (SnoozedBadge, a label and not a control, is
// free to import.)
const PRIMITIVE = "components/AlarmActions.tsx";
const OWN = new Set(["components/AcknowledgeButton.tsx", "components/SnoozeButton.tsx"]);
const FORBIDDEN = /\bimport\s+(?:[^;]*?\b)?(AcknowledgeButton|SnoozeControl|SnoozeButton)\b[^;]*?from\s*["'][^"']*["']|\bimport\s+[^;]*?from\s*["'][^"']*\/AcknowledgeButton["']/g;

function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p, out);
    else if (/\.tsx?$/.test(name) && !/\.test\.tsx?$/.test(name) && !/\.gen\.ts$/.test(name)) out.push(p);
  }
  return out;
}

const stripComments = (src: string) => src.replace(/\/\*[\s\S]*?\*\//g, "").replace(/(^|[^:])\/\/.*$/gm, "$1");
const hits = (src: string) => [...stripComments(src).matchAll(FORBIDDEN)].map((m) => m[0]);

describe("alarm controls come only through AlarmActions", () => {
  it("flags the direct imports and passes the badge", () => {
    expect(hits('import AcknowledgeButton from "../components/AcknowledgeButton";')).toHaveLength(1);
    expect(hits('import { SnoozeControl, SnoozedBadge } from "./SnoozeButton";')).toHaveLength(1);
    expect(hits('import SnoozeButton, { SnoozedBadge } from "./SnoozeButton";')).toHaveLength(1);
    expect(hits('import { SnoozedBadge } from "./SnoozeButton";')).toEqual([]);
    expect(hits('import AlarmActions from "./AlarmActions";')).toEqual([]);
  });

  it("no file but the primitive imports them", () => {
    const found: string[] = [];
    for (const f of walk(__dirname)) {
      const rel = f.replace(__dirname + "/", "");
      if (rel === PRIMITIVE || OWN.has(rel)) continue;
      for (const h of hits(readFileSync(f, "utf8"))) found.push(`${rel}: ${h}`);
    }
    expect(found).toEqual([]);
  });
});
