import type { QueryClient } from "@tanstack/solid-query";
import { rel } from "./format";
import { COMPONENT_ALARMS_PREFIX, FLEET_ALARMS_PREFIX } from "./alarms";
import { SIMULATION_KEY, componentSimKey, impairmentKey, fireSim, impairmentSummary, isImpaired, setImpairment, toImpairment, HEALTHY, type Impairment, type SimComponent, type SimSurface } from "./simulation";

// The fault vocabulary of the Simulation console (#2462): what an operator can
// break on a simulated device, in named actions generated from what its profile
// declares it can do, and what is broken now, read from the simulator. All pure
// except runFaultSteps, the one function that writes.
//
// The profile declares a quick set on an HTTP route (the state path it drives,
// the body field and the values it accepts); everything else here follows from
// that and from the impairment. Pinned values and per-protocol refusal (#909)
// slot in as another group.

// A step is one write: replace the whole impairment, or fire one HTTP request.
export type FaultStep =
  | { kind: "impair"; impairment: Impairment }
  | { kind: "fire"; surface: string; method: string; path: string; body: string };

// The menu's groups, in the order it lays them out. "pinned" is held for #909.
export const FAULT_GROUPS = ["power", "connection", "set", "pinned", "restore"] as const;
export type FaultGroup = (typeof FAULT_GROUPS)[number];
export const FAULT_GROUP_LABEL: Record<FaultGroup, string> = {
  power: "Power",
  connection: "Connection",
  set: "Quick sets",
  pinned: "Pinned values",
  restore: "Restore",
};

export type FaultAction = { id: string; group: FaultGroup; label: string; hint: string; steps: FaultStep[] };

// A state path held off its default, as the simulator reports it.
export type HeldFault = { path: string; value: string; default: string; since: string };
// What is wrong with a device now, the part the actions depend on.
export type FaultState = { impairment: Impairment; faults: readonly HeldFault[] };

type QuickSet = { surface: string; method: string; path: string; field: string; statePath: string; values: string[]; default?: string };

// quickSets is every settable state path of a device, once each, from the
// first surface the console can fire on that sets it.
function quickSets(surfaces: readonly SimSurface[]): QuickSet[] {
  const seen = new Set<string>();
  const out: QuickSet[] = [];
  for (const s of surfaces) {
    const p = s.protocol.toLowerCase();
    if (!s.fireable || (p !== "http" && p !== "https")) continue;
    for (const r of s.routes ?? []) {
      for (const set of r.sets ?? []) {
        if (seen.has(set.path)) continue;
        seen.add(set.path);
        out.push({ surface: s.name, method: r.method, path: r.path, field: set.field, statePath: set.path, values: set.values ?? [], default: set.default });
      }
    }
  }
  return out;
}

// A settable state path as the Info tab's form shows it: what it is called, the
// values the profile accepts, what it holds now and its default, and the steps
// that set it to a value (the same fire a named action makes, so the faults
// and the chip follow).
export type StateField = { path: string; name: string; values: string[]; current: string | undefined; default: string | undefined; stepFor: (value: string) => FaultStep };

// stateFields generates the state form from the profile's quick sets.
export function stateFields(surfaces: readonly SimSurface[], faults: readonly HeldFault[]): StateField[] {
  return quickSets(surfaces).map((q) => ({
    path: q.statePath,
    name: q.statePath.replace(/^status\./, ""),
    values: q.values,
    current: heldValue(q, faults),
    default: q.default,
    stepFor: (value: string) => fireStep(q, value),
  }));
}

const lastSegment = (path: string) => path.slice(path.lastIndexOf(".") + 1);
const isBool = (values: readonly string[]) => values.length === 2 && values.includes("true") && values.includes("false");

// The request body for setting a field to a value: a true/false is sent as a
// boolean, anything else as the text the profile lists.
function setBody(field: string, value: string): string {
  return JSON.stringify({ [field]: value === "true" ? true : value === "false" ? false : value });
}

function fireStep(q: QuickSet, value: string): FaultStep {
  return { kind: "fire", surface: q.surface, method: q.method, path: q.path, body: setBody(q.field, value) };
}

function setLabel(q: QuickSet, value: string): { group: FaultGroup; label: string } {
  const name = lastSegment(q.statePath);
  if (name === "power") return { group: "power", label: value === "standby" ? "Standby" : `Power ${value}` };
  if (name === "mute" && isBool(q.values)) return { group: "set", label: value === "true" ? "Mute" : "Unmute" };
  const title = name.charAt(0).toUpperCase() + name.slice(1);
  return { group: "set", label: `${title}: ${value}` };
}

// heldValue is the value a path holds now: the reported fault's, else its default.
function heldValue(q: QuickSet, faults: readonly HeldFault[]): string | undefined {
  return faults.find((f) => f.path === q.statePath)?.value ?? q.default;
}

// faultActions is the menu of a device: power, unplug or replug, each quick
// set its profile exposes (leaving out the value a path already holds), and
// Restore when anything is wrong. The order is the groups', then the profile's.
export function faultActions(surfaces: readonly SimSurface[], state: FaultState): FaultAction[] {
  const out: FaultAction[] = [];
  for (const q of quickSets(surfaces)) {
    const now = heldValue(q, state.faults);
    for (const value of q.values) {
      if (value === now) continue;
      const { group, label } = setLabel(q, value);
      out.push({ id: `set:${q.statePath}=${value}`, group, label, hint: `Sets ${q.statePath} to ${value} through ${q.method} ${q.path}.`, steps: [fireStep(q, value)] });
    }
  }
  const unplugged = toImpairment(state.impairment).unplugged;
  out.push(
    unplugged
      ? { id: "replug", group: "connection", label: "Replug", hint: "Plug it back in, keeping any latency, loss or rate limit set.", steps: [{ kind: "impair", impairment: { ...toImpairment(state.impairment), unplugged: false } }] }
      : { id: "unplug", group: "connection", label: "Unplug", hint: "Dark on every protocol: connections time out and pings go unanswered.", steps: [{ kind: "impair", impairment: { ...toImpairment(state.impairment), unplugged: true } }] },
  );
  const restore = restoreSteps(surfaces, state);
  if (restore.length) out.push({ id: "restore", group: "restore", label: "Restore", hint: "Repair the impairment and set every held path back to its profile default.", steps: restore });
  return out.sort((a, b) => FAULT_GROUPS.indexOf(a.group) - FAULT_GROUPS.indexOf(b.group));
}

// restoreSteps clears everything: the impairment, and each held path set back
// to its default where a route accepts that value. A path whose default no
// route accepts is left (firing a refused value would only fail).
export function restoreSteps(surfaces: readonly SimSurface[], state: FaultState): FaultStep[] {
  const steps: FaultStep[] = [];
  if (isImpaired(state.impairment)) steps.push({ kind: "impair", impairment: HEALTHY });
  const sets = quickSets(surfaces);
  for (const f of state.faults) {
    const q = sets.find((x) => x.statePath === f.path);
    if (q && q.values.includes(f.default)) steps.push(fireStep(q, f.default));
  }
  return steps;
}

// skippedRestores names the held paths restoreSteps leaves alone: no route
// accepts the default, or the surface no longer has the route. The clear says so.
export function skippedRestores(surfaces: readonly SimSurface[], faults: readonly HeldFault[]): string[] {
  const sets = quickSets(surfaces);
  return faults.filter((f) => !sets.find((x) => x.statePath === f.path)?.values.includes(f.default)).map((f) => f.path);
}

// runFaultSteps does the steps in order and stops at the first that fails.
export async function runFaultSteps(componentId: string, steps: readonly FaultStep[]): Promise<void> {
  for (const s of steps) {
    if (s.kind === "impair") await setImpairment(componentId, s.impairment);
    else {
      const res = await fireSim(componentId, { surface: s.surface, method: s.method, path: s.path, body: s.body });
      if (res.error) throw { detail: res.error };
      if (res.status && res.status >= 400) throw { detail: `The device refused it (${res.status}).` };
    }
  }
}

// A fault as the chip shows it. key is stable across polls.
export type ActiveFault = { key: string; label: string; since: string | undefined };

// age is how long a fault has held, as the chip shows it: "4m", "2h".
export const age = (iso: string): string => rel(iso).replace(/ ago$/, "");

// faultLabel words a held path: "powered off", "muted", "input hdmi2".
export function faultLabel(f: { path: string; value: string }): string {
  const name = lastSegment(f.path);
  if (name === "power") return f.value === "standby" ? "on standby" : f.value === "off" ? "powered off" : `power ${f.value}`;
  if (name === "mute" && (f.value === "true" || f.value === "false")) return f.value === "true" ? "muted" : "unmuted";
  return `${name} ${f.value}`;
}

// activeFaults is what is wrong with a simulated device now, from the
// simulator's reads: the impairment, then the held paths, oldest first.
export function activeFaults(d: Pick<SimComponent, "impairment" | "impairment_since" | "faults">): ActiveFault[] {
  const out: ActiveFault[] = [];
  if (isImpaired(d.impairment)) out.push({ key: "impairment", label: impairmentSummary(d.impairment), since: d.impairment_since });
  for (const f of d.faults ?? []) out.push({ key: f.path, label: faultLabel(f), since: f.since });
  return out.sort((a, b) => (a.since ?? "").localeCompare(b.since ?? ""));
}

// applyFaultSteps runs the steps, then refreshes what shows them: the fleet and
// the component's simulation reads, its impairment, and the alarms the change
// is about to raise or clear.
export async function applyFaultSteps(qc: QueryClient, componentId: string, steps: readonly FaultStep[]): Promise<void> {
  try {
    await runFaultSteps(componentId, steps);
  } finally {
    await Promise.all([
      qc.invalidateQueries({ queryKey: SIMULATION_KEY, exact: true }),
      qc.invalidateQueries({ queryKey: componentSimKey(componentId) }),
      qc.invalidateQueries({ queryKey: impairmentKey(componentId) }),
      qc.invalidateQueries({ queryKey: [...COMPONENT_ALARMS_PREFIX] }),
      qc.invalidateQueries({ queryKey: [...FLEET_ALARMS_PREFIX] }),
    ]);
  }
}
