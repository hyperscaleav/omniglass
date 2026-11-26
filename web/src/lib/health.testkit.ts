import type { QueryClient } from "@tanstack/solid-query";
import { PRESETS } from "./time_range";
import { systemHealthRangeKey } from "./health";

// seedSystemHealth puts one report behind a system's health read over every
// preset range (#2741): the system page reads its health over the page's
// range, so a test that seeds the report once sees it on every tab and after
// any preset pick, without stubbing the read.
// The report is whatever shape the test draws (a partial one included).
export function seedSystemHealth(qc: QueryClient, id: string, report: unknown) {
  for (const p of PRESETS) qc.setQueryData([...systemHealthRangeKey(id, { kind: "preset", preset: p.key })], report);
}
