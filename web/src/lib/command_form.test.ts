import { describe, it, expect } from "vitest";
import { commandControls, controlFromSchema, initialDraft, readDraft, readControl, commandGap } from "./command_form";
import type { CommandTypeRow } from "./command_types";
import type { PropertyRow } from "./properties";
import type { MetricRow } from "./metric_types";
import type { Driver } from "./drivers";
import type { Endpoint } from "./endpoints";

// The command types and the target types as the seed ships them (#2466).
const setPower: CommandTypeRow = { name: "set-power", settle_window_seconds: 15, target_property_type: "power-state", official: true };
const setVolume: CommandTypeRow = { name: "set-volume", settle_window_seconds: 15, target_metric_type: "volume", official: true };
const setMute: CommandTypeRow = { name: "set-mute", settle_window_seconds: 15, target_property_type: "mute", official: true };
const reboot: CommandTypeRow = { name: "reboot", settle_window_seconds: 0, official: true };
const outletOff: CommandTypeRow = {
  name: "outlet-off",
  settle_window_seconds: 0,
  official: true,
  params_schema: { type: "object", required: ["outlet"], properties: { outlet: { type: "integer", minimum: 1, maximum: 4, description: "The outlet to switch." } } },
};
const props: PropertyRow[] = [
  { name: "power-state", data_type: "string", validation: { enum: ["on", "off", "standby"] }, official: true } as PropertyRow,
  { name: "mute", data_type: "string", validation: { enum: ["true", "false"] }, official: true } as PropertyRow,
  { name: "free-note", data_type: "string", official: true } as PropertyRow,
];
const metrics: MetricRow[] = [{ name: "volume", data_type: "float", unit: "ratio", official: true }];
const types = { properties: props, metrics };

describe("commandControls", () => {
  it("offers set-power's on, off and standby as a select, never free text", () => {
    const [c] = commandControls(setPower, types);
    expect(c).toMatchObject({ field: "value", kind: "select", options: ["on", "off", "standby"], required: true });
  });

  it("reads a true/false string enum as a switch carrying the strings", () => {
    const [c] = commandControls(setMute, types);
    expect(c).toMatchObject({ kind: "switch", on: "true", off: "false" });
  });

  it("gives a ratio metric its 0 to 1 range and unit", () => {
    const [c] = commandControls(setVolume, types);
    expect(c).toMatchObject({ kind: "number", integer: false, min: 0, max: 1, unit: "ratio" });
  });

  it("offers a fire-and-forget command no value control", () => {
    expect(commandControls(reboot, types)).toEqual([]);
  });

  it("generates a param control from the params schema, range included", () => {
    const [c] = commandControls(outletOff, types);
    expect(c).toMatchObject({ field: "params.outlet", kind: "number", integer: true, min: 1, max: 4, required: true });
  });

  it("falls back to free text only when the target type declares no values", () => {
    const t: CommandTypeRow = { name: "set-note", settle_window_seconds: 0, target_property_type: "free-note", official: false };
    expect(commandControls(t, types)[0].kind).toBe("text");
  });

  it("falls back to free text when the target types are not readable", () => {
    expect(commandControls(setPower, { properties: undefined, metrics: undefined })[0].kind).toBe("text");
  });
});

describe("controlFromSchema", () => {
  it("maps a boolean to a switch, a number to a number, a string to text", () => {
    expect(controlFromSchema("flag", { type: "boolean" }, {}).kind).toBe("switch");
    expect(controlFromSchema("n", { type: "number", minimum: -5 }, {})).toMatchObject({ kind: "number", min: -5, integer: false });
    expect(controlFromSchema("s", { type: "string", pattern: "^a" }, {})).toMatchObject({ kind: "text", pattern: "^a" });
  });
  it("makes any enum a select, numbers included", () => {
    expect(controlFromSchema("e", { enum: [1, 2, 3] }, {})).toMatchObject({ kind: "select", options: ["1", "2", "3"] });
  });
});

describe("readControl", () => {
  const [power] = commandControls(setPower, types);
  const [volume] = commandControls(setVolume, types);
  const [outlet] = commandControls(outletOff, types);
  const [mute] = commandControls(setMute, types);

  it("refuses a select with nothing chosen and accepts a choice", () => {
    expect(readControl(power, "").error).toBeTruthy();
    expect(readControl(power, "off")).toEqual({ value: "off" });
  });

  it("refuses an out-of-range number and says the range", () => {
    expect(readControl(volume, "1.5").error).toMatch(/0 to 1/);
    expect(readControl(volume, "-0.1").error).toBeTruthy();
    expect(readControl(volume, "0.35")).toEqual({ value: 0.35 });
    expect(readControl(volume, "abc").error).toBeTruthy();
    expect(readControl(volume, "").error).toBeTruthy();
  });

  it("refuses a fractional or out-of-range integer", () => {
    expect(readControl(outlet, "2.5").error).toBeTruthy();
    expect(readControl(outlet, "5").error).toMatch(/1 to 4/);
    expect(readControl(outlet, "3")).toEqual({ value: 3 });
  });

  it("reads a switch as its on or off value", () => {
    expect(readControl(mute, "on")).toEqual({ value: "true" });
    expect(readControl(mute, "off")).toEqual({ value: "false" });
  });

  it("keeps free text's JSON scalar reading for an undeclared property", () => {
    const [t] = commandControls({ name: "x", settle_window_seconds: 0, target_property_type: "free-note", official: false }, types);
    expect(readControl(t, "42")).toEqual({ value: 42 });
    expect(readControl(t, "hello")).toEqual({ value: "hello" });
    expect(readControl(t, "").error).toBeTruthy();
  });
});

describe("drafts", () => {
  it("starts a switch off and everything else empty, and reads a whole draft", () => {
    const controls = [...commandControls(setMute, types), ...commandControls(outletOff, types)];
    const d = initialDraft(controls);
    expect(d).toEqual({ value: "off", "params.outlet": "" });
    expect(readDraft(controls, d).error).toBeTruthy();
    expect(readDraft(controls, { ...d, "params.outlet": "2" })).toMatchObject({ value: "false", params: { outlet: 2 } });
  });
});

describe("commandGap", () => {
  const drv = (name: string, commands: string[]): Driver =>
    ({ id: name, name, label: name, official: true, spec: { version: 1, transport: "x", commands: commands.map((c) => ({ command_type: c, request: {} })) } }) as Driver;
  const ep = (driver: string): Endpoint => ({ component_id: "c-1", driver }) as Endpoint;
  const drivers = [drv("snmp-v2c", []), drv("kestrel-api", ["set-volume", "set-mute"]), drv("lumen-http", ["set-power"])];

  it("names the attached drivers that bind nothing and the drivers that would", () => {
    const g = commandGap("c-1", [ep("snmp-v2c")], drivers, undefined);
    expect(g.attachedWithout).toEqual(["snmp-v2c"]);
    expect(g.offering.map((o) => o.driver)).toEqual(["kestrel-api", "lumen-http"]);
    expect(g.offering[1].commands).toEqual(["set-power"]);
    expect(g.productDriver).toBeUndefined();
  });

  it("puts the product's own driver first when it binds commands", () => {
    const g = commandGap("c-1", [ep("snmp-v2c")], drivers, "lumen-http");
    expect(g.productDriver).toEqual({ driver: "lumen-http", commands: ["set-power"] });
    expect(g.offering[0].driver).toBe("lumen-http");
  });

  it("leaves another component's endpoints out", () => {
    const other = { component_id: "c-2", driver: "snmp-v2c" } as Endpoint;
    expect(commandGap("c-1", [other], drivers, undefined).attachedWithout).toEqual([]);
  });
});
