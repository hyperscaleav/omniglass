import { describe, expect, it } from "vitest";
import {
  driverCollects,
  driversCollecting,
  driversOffering,
  exploreHref,
  rulesReading,
  writerLabel,
} from "./catalog_usage";
import type { Driver } from "./drivers";

const driver = (name: string, spec?: Driver["spec"]): Driver => ({ id: `id-${name}`, name, label: name, official: true, spec });

const snmp = driver("boreal-beam-snmp", {
  version: 1,
  transport: "snmp",
  polls: [
    { name: "identity", schedule: { every: "5m" }, request: {}, emits: [{ name: "model-number", extract: {} }] },
    { name: "status", schedule: { every: "30s" }, request: {}, emits: [{ name: "uptime", extract: {} }, { name: "power-state", extract: {} }, { name: "mystery", extract: {} }] },
  ],
  commands: [{ command_type: "power-on", request: {} }, { command_type: "power-off", request: {} }],
});
const listener = driver("kestrel-ws", {
  version: 1,
  transport: "ws",
  listeners: [{ name: "push", match: { prefix: "x" }, emits: [{ name: "uptime", extract: {} }] }],
});

describe("driverCollects", () => {
  const metrics = new Set(["uptime"]);
  const properties = new Set(["model-number", "power-state"]);

  it("sorts what a driver emits into metrics, properties, and keys the catalogs do not know, and lists its commands", () => {
    expect(driverCollects(snmp.spec, metrics, properties)).toEqual({
      metrics: ["uptime"],
      properties: ["model-number", "power-state"],
      unknown: ["mystery"],
      commands: ["power-off", "power-on"],
    });
  });

  it("counts a listener's emits beside a poll's and names each key once", () => {
    expect(driverCollects(listener.spec, metrics, properties).metrics).toEqual(["uptime"]);
  });

  it("is empty for a stub with no spec", () => {
    expect(driverCollects(undefined, metrics, properties)).toEqual({ metrics: [], properties: [], unknown: [], commands: [] });
  });
});

describe("driver reverse lookups", () => {
  it("finds the drivers that emit a key, polls and listeners both", () => {
    expect(driversCollecting([snmp, listener], "uptime").map((d) => d.name)).toEqual(["boreal-beam-snmp", "kestrel-ws"]);
    expect(driversCollecting([snmp, listener], "model-number").map((d) => d.name)).toEqual(["boreal-beam-snmp"]);
    expect(driversCollecting([snmp, listener], "nothing")).toEqual([]);
  });

  it("finds the drivers that offer a command type", () => {
    expect(driversOffering([snmp, listener], "power-on").map((d) => d.name)).toEqual(["boreal-beam-snmp"]);
    expect(driversOffering([snmp, listener], "reboot")).toEqual([]);
  });
});

describe("rulesReading", () => {
  const event = [
    { name: "a", sample_key: "uptime" },
    { name: "b", sample_key: "temp" },
  ];
  const calc = [
    { name: "c", inputs: [{ alias: "t", key: "temp" }], output_key: "temp-mean" },
    { name: "d", inputs: [{ alias: "u", key: "uptime" }], output_key: "x" },
  ];

  it("splits the event rules and the calc rules that read a key", () => {
    const got = rulesReading(event, calc, "temp");
    expect(got.event.map((r) => r.name)).toEqual(["b"]);
    expect(got.calc.map((r) => r.name)).toEqual(["c"]);
  });
});

describe("writerLabel", () => {
  it("names a calc rule first, then the writer class, then nothing", () => {
    expect(writerLabel({ written_by: { rule: "room-mean", rule_id: "r1" }, writer_class: "driver" })).toEqual({ kind: "rule", rule: "room-mean", rule_id: "r1" });
    expect(writerLabel({ writer_class: "outpost-probe" })).toMatchObject({ kind: "class", text: "outpost probe" });
    expect(writerLabel({ writer_class: "collected" })).toMatchObject({ kind: "class", text: "driver" });
    expect(writerLabel({})).toBeNull();
  });
});

describe("exploreHref", () => {
  it("opens Explore with one equals chip over the given values", () => {
    const href = exploreHref("product", ["Boreal Beam 340", "Boreal Edge 55"]);
    const q = new URL(href, "http://x").searchParams;
    expect(href.startsWith("/explore?")).toBe(true);
    expect(JSON.parse(q.get("chips")!)).toEqual([{ key: "product", op: "eq", values: ["Boreal Beam 340", "Boreal Edge 55"] }]);
  });
});
