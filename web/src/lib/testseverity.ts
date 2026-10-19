import type { QueryClient } from "@tanstack/solid-query";
import { severityLevelsKey, type SeverityLevel } from "./severity";
import { uuidFor } from "./testids";

// The seeded severity registry (#917) as a test fixture: the five shipped levels,
// least to most severe, exactly as GET /severity-levels serves them. Tests seed it
// into the query cache so every severity badge, sort and outage check reads the
// registry the server ships rather than a value hardcoded in the console.
export const SEEDED_LEVELS: SeverityLevel[] = [
  { id: uuidFor("sev-info"), name: "info", label: "Info", color: "gray", rank: 10, official: true, outage: false },
  { id: uuidFor("sev-warning"), name: "warning", label: "Warning", color: "yellow", rank: 20, official: true, outage: false },
  { id: uuidFor("sev-average"), name: "average", label: "Average", color: "orange", rank: 30, official: true, outage: false },
  { id: uuidFor("sev-high"), name: "high", label: "High", color: "red", rank: 40, official: true, outage: true },
  { id: uuidFor("sev-disaster"), name: "disaster", label: "Disaster", color: "darkred", rank: 50, official: true, outage: true },
];

// An operator-defined level between average and high: proves a level the console
// has never heard of renders, sorts and gates by the registry alone.
export const MAJOR_LEVEL: SeverityLevel = { id: uuidFor("sev-major"), name: "major", label: "Major", color: "orange", rank: 35, official: false, outage: false };

export function seedSeverityLevels(qc: QueryClient, levels: SeverityLevel[] = SEEDED_LEVELS): void {
  qc.setQueryData([...severityLevelsKey], levels);
}
