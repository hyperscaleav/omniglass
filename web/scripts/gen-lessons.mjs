// Writes the console's lessons (web/src/lib/lessons.ts, #2450) as
// docs/src/generated/lessons.json, so the docs page that lists them renders
// from the same definitions the Learn page does (make gen; gen-drift holds the
// committed file to a fresh run). lessons.ts is pure data with no imports, so
// node loads it directly with its types stripped.
import { writeFileSync } from "node:fs";

const { LESSONS, CAPABILITIES, stepFor } = await import(new URL("../src/lib/lessons.ts", import.meta.url).href);

const out = {
  lessons: LESSONS.map((l, i) => ({
    number: i + 1,
    slug: l.slug,
    title: l.title,
    teaches: l.teaches,
    minutes: l.minutes,
    needsSimulator: l.needsSimulator,
    steps: l.steps.map((s) => {
      const now = stepFor(s, CAPABILITIES);
      return { title: now.title, prose: now.prose, live: now.live ?? null, link: now.link };
    }),
  })),
};

writeFileSync(new URL("../../docs/src/generated/lessons.json", import.meta.url), JSON.stringify(out, null, 2) + "\n");
console.log("wrote docs/src/generated/lessons.json");
