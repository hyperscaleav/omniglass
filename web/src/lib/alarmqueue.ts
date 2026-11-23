import type { Chip, FilterKey } from "./predicate";
import type { Alarm, FleetAlarmsQuery } from "./alarms";

// The alarm queue's filter model (#2333). The page keeps its filter in the
// address (`/alarms?state=active&severity=critical&ack=no`) so the page can be
// handed to a colleague or to omnibot (pageSubject) and read back the same.
// Four facets, each a chip: severity (one or more levels), location (a subtree,
// by name), ack (yes | no) and snoozed (yes | no). The state is
// not a chip: `state=active` is the queue an operator works, `state=cleared`
// and `state=all` read the history (#2426), each cleared row with when it
// cleared and how long it was raised.

export type AlarmState = "active" | "cleared" | "all";
export const ALARM_STATES: AlarmState[] = ["active", "cleared", "all"];

// stateFromParams reads the address's state; anything unknown is the queue.
export function stateFromParams(p: Params): AlarmState {
  const v = list(p.state)[0];
  return (ALARM_STATES as string[]).includes(v) ? (v as AlarmState) : "active";
}

export const SEVERITY = "severity";
export const LOCATION = "location";
export const ACK = "ack";
export const SNOOZED = "snoozed";

const YES_NO = ["yes", "no"];

type Params = Record<string, string | string[] | undefined>;

const list = (v: string | string[] | undefined): string[] =>
  (Array.isArray(v) ? v : v === undefined ? [] : [v]).flatMap((s) => s.split(",")).map((s) => s.trim()).filter(Boolean);

// chipsFromParams reads the address into chips. Anything it does not know is
// dropped, so a hand-edited link reads as fewer filters, never as an error.
export function chipsFromParams(p: Params): Chip[] {
  const chips: Chip[] = [];
  const sev = list(p.severity);
  if (sev.length) chips.push({ key: SEVERITY, op: "eq", values: sev });
  const loc = list(p.location)[0];
  if (loc) chips.push({ key: LOCATION, op: "eq", values: [loc] });
  for (const [key, param] of [[ACK, p.ack], [SNOOZED, p.snoozed]] as const) {
    const v = list(param)[0];
    if (v && YES_NO.includes(v)) chips.push({ key, op: "eq", values: [v] });
  }
  return chips;
}

// paramsFromChips writes chips back to the address: the inverse of
// chipsFromParams, `state=active` always first so the link names what it shows.
export function paramsFromChips(chips: Chip[], state: AlarmState = "active"): Record<string, string | undefined> {
  const out: Record<string, string | undefined> = { state, severity: undefined, location: undefined, ack: undefined, snoozed: undefined };
  for (const c of chips) {
    if (c.op !== "eq" || !c.values.length) continue;
    if (c.key === SEVERITY) out.severity = c.values.join(",");
    else if (c.key === LOCATION) out.location = c.values[0];
    else if (c.key === ACK || c.key === SNOOZED) out[c.key] = c.values[0];
  }
  return out;
}

// queryFromChips is the server's own filter for the chips: the queue is
// narrowed where it is read, so a filter holds over a queue longer than the
// pages loaded, and the rows that land are already the filtered set.
export function queryFromChips(chips: Chip[], state: AlarmState = "active"): FleetAlarmsQuery {
  const q: FleetAlarmsQuery = {};
  if (state !== "active") q.state = state;
  for (const c of chips) {
    if (c.op !== "eq" || !c.values.length) continue;
    if (c.key === SEVERITY) q.severity = c.values;
    else if (c.key === LOCATION) q.location = c.values[0];
    else if (c.key === ACK) q.acknowledged = c.values[0] === "yes";
    else if (c.key === SNOOZED) q.snoozed = c.values[0] === "yes";
  }
  return q;
}

// alarmFilterKeys is the FilterBar's vocabulary for the queue. The facets the
// server narrows (severity, location, ack, snoozed) are offered as chips; the
// free-text fallback ("component") filters the loaded rows by name, system or
// message. The location getter answers the chip's own value when the row sits
// anywhere under it, because the server's location filter is a subtree and
// the client check over the loaded rows must agree with it.
export function alarmFilterKeys(chips: () => Chip[], severities: () => string[]): FilterKey<Alarm>[] {
  const locChip = () => chips().find((c) => c.key === LOCATION)?.values[0]?.toLowerCase();
  return [
    { key: "component", type: "string", hint: "substring", get: (a) => [a.component, a.message, ...(a.systems ?? [])].join(" ") },
    { key: SEVERITY, type: "string", hint: "exact", get: (a) => a.severity, values: severities },
    {
      key: LOCATION, type: "string", hint: "exact",
      get: (a) => {
        const want = locChip();
        const path = (a.location_path ?? "").split(".");
        return want && path.some((s) => s.toLowerCase() === want) ? want : (a.location ?? "");
      },
      values: (rows) => [...new Set(rows.map((a) => a.location).filter((l): l is string => !!l))].sort(),
    },
    { key: ACK, type: "string", hint: "exact", get: (a) => (a.acknowledged ? "yes" : "no"), values: () => YES_NO },
    { key: SNOOZED, type: "string", hint: "exact", get: (a) => (a.snoozed_until ? "yes" : "no"), values: () => YES_NO },
  ];
}
