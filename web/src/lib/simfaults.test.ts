import { describe, expect, it } from "vitest";
import { HEALTHY, type SimComponent, type SimSurface } from "./simulation";
import { activeFaults, faultActions, stateFields, faultLabel, restoreSteps, type FaultAction } from "./simfaults";

// A display's profile as the API lists it: an http surface with the power and
// input routes (each declaring its quick sets), an snmp surface that has none.
const display: SimSurface[] = [
  {
    name: "http", protocol: "http", port: 80, fireable: true,
    routes: [
      { method: "GET", path: "/api/status" },
      { method: "POST", path: "/api/display/power", sets: [{ path: "status.device.power", field: "power", values: ["on", "off", "standby"], default: "on" }] },
      { method: "POST", path: "/api/display/input", sets: [{ path: "status.display.input", field: "input", values: ["hdmi1", "hdmi2"], default: "hdmi1" }] },
    ],
  },
  { name: "snmp", protocol: "snmp", port: 161, fireable: true, oids: [{ oid: "1.3.6", path: "status.device.power" }] },
];
const mic: SimSurface[] = [
  { name: "http", protocol: "http", port: 80, fireable: true, routes: [{ method: "POST", path: "/api/mute", sets: [{ path: "status.mic.mute", field: "mute", values: ["true", "false"], default: "false" }] }] },
];
const healthy = { impairment: HEALTHY, faults: [] };
const labels = (a: FaultAction[]) => a.map((x) => x.label);

describe("faultActions", () => {
  it("offers a device the actions its profile can do, grouped power, connection, then quick sets", () => {
    const a = faultActions(display, healthy);
    expect(labels(a)).toEqual(["Power off", "Standby", "Unplug", "Input: hdmi2"]);
    expect(a.map((x) => x.group)).toEqual(["power", "power", "connection", "set"]);
  });

  it("leaves out the value a path already holds, and offers the way back instead", () => {
    const a = faultActions(display, { impairment: { ...HEALTHY, unplugged: true }, faults: [{ path: "status.device.power", value: "off", default: "on", since: "2026-11-23T12:00:00Z" }] });
    expect(labels(a)).toEqual(["Power on", "Standby", "Replug", "Input: hdmi2", "Restore"]);
  });

  it("offers Restore only while something is wrong", () => {
    expect(labels(faultActions(display, healthy))).not.toContain("Restore");
    expect(labels(faultActions(display, { impairment: { ...HEALTHY, latency_ms: 200 }, faults: [] }))).toContain("Restore");
  });

  it("each action is the steps that do it: an impairment write, or a fire on the route with the body typed for the operator", () => {
    const by = Object.fromEntries(faultActions(display, healthy).map((x) => [x.label, x.steps]));
    expect(by["Power off"]).toEqual([{ kind: "fire", surface: "http", method: "POST", path: "/api/display/power", body: '{"power":"off"}' }]);
    expect(by["Unplug"]).toEqual([{ kind: "impair", impairment: { ...HEALTHY, unplugged: true } }]);
  });

  it("words a mute as Mute and Unmute, and sends a boolean", () => {
    const a = faultActions(mic, healthy);
    expect(labels(a)).toEqual(["Unplug", "Mute"]);
    expect(a[1].steps).toEqual([{ kind: "fire", surface: "http", method: "POST", path: "/api/mute", body: '{"mute":true}' }]);
    const muted = faultActions(mic, { impairment: HEALTHY, faults: [{ path: "status.mic.mute", value: "true", default: "false", since: "2026-11-23T12:00:00Z" }] });
    expect(labels(muted)).toEqual(["Unplug", "Unmute", "Restore"]);
  });

  it("offers a path once even when two surfaces set it, and never fires on a surface the console cannot", () => {
    const twice: SimSurface[] = [display[0], { ...display[0], name: "https", protocol: "https" }, { ...display[0], name: "tcp", protocol: "tcp", fireable: false }];
    expect(labels(faultActions(twice, healthy))).toEqual(["Power off", "Standby", "Unplug", "Input: hdmi2"]);
  });

  it("a device with no settable path still offers Unplug", () => {
    expect(labels(faultActions([display[1]], healthy))).toEqual(["Unplug"]);
  });
});

describe("restoreSteps", () => {
  it("repairs the impairment and sets each held path back to its default, and nothing for a healthy device", () => {
    expect(restoreSteps(display, healthy)).toEqual([]);
    const steps = restoreSteps(display, {
      impairment: { ...HEALTHY, unplugged: true },
      faults: [
        { path: "status.device.power", value: "off", default: "on", since: "2026-11-23T12:00:00Z" },
        { path: "status.display.input", value: "hdmi2", default: "hdmi1", since: "2026-11-23T12:00:00Z" },
      ],
    });
    expect(steps).toEqual([
      { kind: "impair", impairment: HEALTHY },
      { kind: "fire", surface: "http", method: "POST", path: "/api/display/power", body: '{"power":"on"}' },
      { kind: "fire", surface: "http", method: "POST", path: "/api/display/input", body: '{"input":"hdmi1"}' },
    ]);
  });

  it("skips a held path whose default no route accepts, rather than firing a refused value", () => {
    const steps = restoreSteps(display, { impairment: HEALTHY, faults: [{ path: "status.device.power", value: "off", default: "unknown", since: "2026-11-23T12:00:00Z" }] });
    expect(steps).toEqual([]);
  });
});

describe("activeFaults", () => {
  const now = "2026-11-23T12:00:00Z";
  const sim = (p: Partial<SimComponent>): SimComponent => ({ component_id: "c", component: "c", product: "p", address: "a", serving: true, status: "running", hits: 0, hits_by_protocol: {}, impaired: true, ...p });

  it("is empty for a healthy device", () => {
    expect(activeFaults(sim({}))).toEqual([]);
  });

  it("reads the simulator's held paths and the impairment as one list, oldest first, each with its start", () => {
    const got = activeFaults(sim({
      impairment: { unplugged: true, latency_ms: 0, jitter_ms: 0, loss_pct: 0, rate_kbit: 0 },
      impairment_since: "2026-11-23T11:00:00Z",
      faults: [{ path: "status.device.power", value: "off", default: "on", since: now }],
    }));
    expect(got.map((f) => [f.label, f.since])).toEqual([["unplugged", "2026-11-23T11:00:00Z"], ["powered off", now]]);
  });

  it("gives an impairment with no recorded start no age", () => {
    const got = activeFaults(sim({ impairment: { unplugged: false, latency_ms: 150, jitter_ms: 0, loss_pct: 2.5, rate_kbit: 0 } }));
    expect(got).toEqual([{ key: "impairment", label: "150 ms, 2.5% loss", since: undefined }]);
  });
});

describe("faultLabel", () => {
  it("says what happened to the device in the operator's words", () => {
    expect(faultLabel({ path: "status.device.power", value: "off" })).toBe("powered off");
    expect(faultLabel({ path: "status.amp.power", value: "standby" })).toBe("on standby");
    expect(faultLabel({ path: "status.mic.mute", value: "true" })).toBe("muted");
    expect(faultLabel({ path: "status.display.input", value: "hdmi2" })).toBe("input hdmi2");
    expect(faultLabel({ path: "status.projector.shutter", value: "closed" })).toBe("shutter closed");
  });
});

describe("stateFields", () => {
  it("is the settable paths with what each holds now, and sets one through the same fire a named action makes", () => {
    const held = [{ path: "status.device.power", value: "off", default: "on", since: "2026-11-23T12:00:00Z" }];
    const f = stateFields(display, held);
    expect(f.map((x) => [x.name, x.current, x.default])).toEqual([["device.power", "off", "on"], ["display.input", "hdmi1", "hdmi1"]]);
    expect(f[0].values).toEqual(["on", "off", "standby"]);
    expect(f[0].stepFor("standby")).toEqual({ kind: "fire", surface: "http", method: "POST", path: "/api/display/power", body: '{"power":"standby"}' });
  });
});
