import { describe, it, expect } from "vitest";
import { inputRows, paramRows, boundSecrets, pollRows, ENDPOINT_FIELDS } from "./endpoint_detail";
import type { Endpoint } from "./endpoints";
import type { DriverSpec } from "./drivers";
import type { EffectiveMetric } from "./component_metrics";

const spec: DriverSpec = {
  version: 1,
  transport: "ssh",
  inputs: [
    { name: "host", kind: "string", required: true },
    { name: "port", kind: "number", default: "22" },
    { name: "login", kind: "secret", secret_type: "ssh-login", required: true },
    { name: "banner", kind: "string" },
  ],
  polls: [
    { name: "status", schedule: { every: "30s" }, request: {}, emits: [{ name: "cpu-load", extract: {} }, { name: "uptime", extract: {} }] },
    { name: "idle", schedule: { every: "5m" }, request: {}, emits: [{ name: "gone", extract: {} }] },
  ],
};
const ep = (over: Partial<Endpoint> = {}): Endpoint => ({
  id: "e1", name: "ssh", transport: "ssh", driver: "panel-ssh",
  params: { target: "10.0.0.5", port: 22 },
  inputs: { host: "10.0.0.5", port: "22", login: "sim-panel-login" },
  ...over,
});

describe("inputRows", () => {
  it("names each declared input with its provenance", () => {
    const rows = inputRows(ep(), spec);
    expect(rows.map((r) => [r.name, r.provenance])).toEqual([
      ["host", "set on this endpoint"],
      ["port", "driver default"],
      ["login", "set on this endpoint"],
      ["banner", "not set"],
    ]);
  });
  it("marks a secret input and carries its reference name, not a value", () => {
    const login = inputRows(ep(), spec).find((r) => r.name === "login")!;
    expect(login.secret).toBe(true);
    expect(login.value).toBe("sim-panel-login");
  });
  it("keeps an input the driver no longer declares", () => {
    const rows = inputRows(ep({ inputs: { host: "h", legacy: "x" } }), spec);
    expect(rows.find((r) => r.name === "legacy")?.provenance).toBe("set on this endpoint");
  });
  it("is empty for a bare probe endpoint", () => {
    expect(inputRows(ep({ driver: undefined, inputs: undefined }), undefined)).toEqual([]);
  });
});

describe("boundSecrets", () => {
  it("lists only the names of secrets that are bound", () => {
    expect(boundSecrets(inputRows(ep(), spec))).toEqual([{ input: "login", secret: "sim-panel-login", type: "ssh-login" }]);
    expect(boundSecrets(inputRows(ep({ inputs: { host: "h" } }), spec))).toEqual([]);
  });
});

describe("paramRows", () => {
  it("shows what a bare probe endpoint was given", () => {
    const rows = paramRows(ep({ driver: undefined, params: { target: "10.0.0.5", timeout: "2s" } }));
    expect(rows).toEqual([
      { name: "target", value: "10.0.0.5", provenance: "set on this endpoint" },
      { name: "timeout", value: "2s", provenance: "set on this endpoint" },
    ]);
  });
  it("reads a driver endpoint's params as derived from its inputs", () => {
    expect(paramRows(ep()).every((r) => r.provenance === "derived from inputs")).toBe(true);
  });
  it("is empty when there are none", () => {
    expect(paramRows(ep({ params: undefined }))).toEqual([]);
  });
});

describe("pollRows", () => {
  const metric = (over: Partial<EffectiveMetric>): EffectiveMetric => ({
    data_type: "float", from_contract: true, instance: "", instance_count: 0, is_sampled: false, metric_type_id: "m", metric_type_name: "x", required: false, ...over,
  });
  const metrics: EffectiveMetric[] = [
    metric({ metric_type_name: "cpu-load", unit: "percent", instance: "ssh", instance_count: 1, is_sampled: true, value: 12.5, sample_value: 12.5 }),
    metric({ metric_type_name: "uptime", unit: "s", instance: "tcp", instance_count: 1, is_sampled: true, value: 9 }),
    metric({
      metric_type_name: "poll-duration", unit: "ms", instance: "ssh", instance_count: 2, is_sampled: true, value: 41,
      instances: [{ endpoint: true, instance: "ssh", ts: "2026-11-23T10:00:00Z", value: 41 }, { endpoint: true, instance: "tcp", ts: "2026-11-23T09:00:00Z", value: 7 }],
    }),
  ];
  it("gives each poll its interval, last read values and duration", () => {
    const [status, idle] = pollRows(spec, "ssh", metrics, [], "2026-11-23T10:00:05Z");
    expect(status).toMatchObject({ name: "status", every: "30s", result: "read 1 of 2 values", duration: { value: 41, unit: "ms" }, lastRun: "2026-11-23T10:00:00Z" });
    expect(status.reads).toEqual([{ name: "cpu-load", value: 12.5, unit: "percent" }]);
    expect(idle).toMatchObject({ name: "idle", every: "5m", result: "nothing read yet", reads: [] });
  });
  it("reads a property the poll emits from what this endpoint observed", () => {
    const props = [
      { property_type_name: "uptime", reconciliation: { is: "3d", is_instance: "ssh", drift: false, want: null, told: null } },
      { property_type_name: "gone", reconciliation: { is: "x", is_instance: "tcp", drift: false, want: null, told: null } },
    ] as never;
    const [status, idle] = pollRows(spec, "ssh", [], props, null);
    expect(status.reads).toEqual([{ name: "uptime", value: "3d" }]);
    expect(idle.reads).toEqual([]);
  });
  it("falls back to the endpoint's last delivery for the run time", () => {
    const [status] = pollRows(spec, "tcp", [], [], "2026-11-23T10:00:05Z");
    expect(status.lastRun).toBe("2026-11-23T10:00:05Z");
  });
  it("is empty without a spec", () => {
    expect(pollRows(undefined, "ssh", metrics, [], null)).toEqual([]);
  });
});

describe("ENDPOINT_FIELDS", () => {
  it("is the one list of field ids both hosts must render", () => {
    expect(new Set(ENDPOINT_FIELDS).size).toBe(ENDPOINT_FIELDS.length);
    for (const f of ["identity", "driver", "availability", "ladder", "session", "parameters", "inputs", "secrets", "out-of-band", "polls"]) expect(ENDPOINT_FIELDS).toContain(f);
  });
});
