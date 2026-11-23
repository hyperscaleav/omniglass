import type { Endpoint } from "./endpoints";
import type { DriverSpec } from "./drivers";
import type { EffectiveMetric } from "./component_metrics";
import type { EffectiveProperty } from "./component_properties";

// The pure half of the endpoint detail body (#2436): everything the body
// states about an endpoint that is derived rather than read, so the component
// is a drawing of it. The body is EndpointDetail.tsx; both hosts (the
// component page's expanded endpoint row and the endpoint drawer) mount it.

// ENDPOINT_FIELDS is the one list of field ids the body renders, each on a
// data-field attribute. The parity test reads the body for every id here in
// both hosts, so a field added to one host and not the other fails.
export const ENDPOINT_FIELDS = [
  "identity",
  "driver",
  "availability",
  "ladder",
  "session",
  "parameters",
  "inputs",
  "secrets",
  "out-of-band",
  "polls",
] as const;
export type EndpointField = (typeof ENDPOINT_FIELDS)[number];

export type Provenance = "set on this endpoint" | "driver default" | "derived from inputs" | "not set";

export type InputRow = { name: string; value?: string; provenance: Provenance; secret: boolean; secretType?: string; required: boolean };

// inputRows is every input the driver declares, in its order, with where the
// value came from: given on this endpoint, the driver's own default standing
// in, or not set. An input the driver no longer declares but the endpoint still
// holds is kept (it is still what the endpoint runs with).
export function inputRows(ep: Endpoint, spec: DriverSpec | undefined): InputRow[] {
  if (!ep.driver) return [];
  const given = ep.inputs ?? {};
  const rows: InputRow[] = (spec?.inputs ?? []).map((d) => {
    const v = given[d.name];
    const has = v !== undefined && v !== "";
    const isDefault = has && d.default !== undefined && d.default !== "" && v === d.default;
    return {
      name: d.name,
      value: has ? v : d.default || undefined,
      provenance: !has ? (d.default ? "driver default" : "not set") : isDefault ? "driver default" : "set on this endpoint",
      secret: d.kind === "secret",
      secretType: d.secret_type,
      required: !!d.required,
    };
  });
  const declared = new Set(rows.map((r) => r.name));
  for (const [name, value] of Object.entries(given)) {
    if (!declared.has(name)) rows.push({ name, value, provenance: "set on this endpoint", secret: false, required: false });
  }
  return rows;
}

// boundSecrets are the secrets the endpoint authenticates with: the NAME of
// each (the reference a secret input holds), never a value.
export function boundSecrets(rows: InputRow[]): { input: string; secret: string; type?: string }[] {
  return rows
    .filter((r) => r.secret && r.value)
    .map((r) => ({ input: r.name, secret: r.value!, ...(r.secretType ? { type: r.secretType } : {}) }));
}

export type ParamRow = { name: string; value: string; provenance: Provenance };

// paramRows are the endpoint's own params. On a driver endpoint they are
// derived from its inputs (the target from host and port), on a bare probe
// endpoint they are what the operator typed.
export function paramRows(ep: Endpoint): ParamRow[] {
  const provenance: Provenance = ep.driver ? "derived from inputs" : "set on this endpoint";
  return Object.entries(ep.params ?? {}).map(([name, v]) => ({ name, value: typeof v === "string" ? v : JSON.stringify(v), provenance }));
}

export type PollRow = {
  name: string;
  every: string;
  lastRun: string | null;
  result: string;
  reads: { name: string; value: unknown; unit?: string }[];
  duration?: { value: number; unit: string };
};

// pollRows draw each declared poll from reads the console already holds: its
// interval from the driver's spec, what it last read from the component's
// effective metrics and properties (the series this endpoint instance carries), its mean
// duration from the endpoint's own poll-duration series, and when it last ran
// from the newest sample time for this endpoint, falling back to the endpoint's
// last delivery. There is no per-poll run record on the server; a run now
// (PollTest) shows the exact read.
export function pollRows(spec: DriverSpec | undefined, endpoint: string, metrics: EffectiveMetric[], properties: EffectiveProperty[], lastSeen: string | null): PollRow[] {
  const mine = (m: EffectiveMetric) => m.instance === endpoint || !!m.instances?.some((i) => i.instance === endpoint);
  const valueFor = (m: EffectiveMetric): unknown => {
    const inst = m.instances?.find((i) => i.instance === endpoint);
    return inst ? inst.value : m.instance === endpoint ? (m.sample_value ?? m.value) : undefined;
  };
  const tsFor = (m: EffectiveMetric): string | undefined => m.instances?.find((i) => i.instance === endpoint)?.ts;
  const dur = metrics.find((m) => m.metric_type_name === "poll-duration" && mine(m));
  const durValue = dur ? valueFor(dur) : undefined;
  return (spec?.polls ?? []).map((p) => {
    const reads: PollRow["reads"] = [];
    let newest: string | undefined;
    for (const e of p.emits) {
      // An emit lands as a metric sample or, for a declared property, as the
      // property's observed value (reconciliation.is, by the instance that saw it).
      const m = metrics.find((x) => x.metric_type_name === e.name && mine(x));
      const v = m ? valueFor(m) : undefined;
      if (m && v !== undefined && v !== null) {
        reads.push({ name: e.name, value: v, ...(m.unit ? { unit: m.unit } : {}) });
        const ts = tsFor(m);
        if (ts && (!newest || ts > newest)) newest = ts;
        continue;
      }
      const prop = properties.find((x) => x.property_type_name === e.name && x.reconciliation?.is_instance === endpoint);
      const observed = prop?.reconciliation?.is;
      if (observed !== undefined && observed !== null) reads.push({ name: e.name, value: observed });
    }
    const lastRun = newest ?? (reads.length ? (tsFor(dur ?? ({} as EffectiveMetric)) ?? lastSeen) : lastSeen);
    return {
      name: p.name,
      every: p.schedule.every,
      lastRun,
      result: reads.length ? `read ${reads.length} of ${p.emits.length} values` : "nothing read yet",
      reads,
      ...(typeof durValue === "number" ? { duration: { value: durValue, unit: dur?.unit ?? "ms" } } : {}),
    };
  });
}
