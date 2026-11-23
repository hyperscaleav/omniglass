import { describe, it, expect, vi } from "vitest";
import {
  STEPS,
  commitFlow,
  driverChoices,
  emptyDraft,
  failureStep,
  inheritedLocation,
  productOption,
  secretOptions,
  simChoice,
  slotsFor,
  splitInputs,
  stepReady,
  undoCreated,
  type FlowDraft,
  type FlowIO,
} from "./add_component_flow";
import type { Driver } from "./drivers";
import type { Product } from "./products";
import type { ComponentType } from "./component_types";
import { uuidFor } from "./testids";

// The add-a-component flow (#2460): what each step needs, which driver the
// product suggests, which secrets fit, where a failed test sends the operator,
// and the create/undo sequence that leaves nothing behind on a cancel.

const types: ComponentType[] = [
  { id: uuidFor("t1"), name: "device", label: "Device", official: true, forked: false },
  { id: uuidFor("t2"), name: "display", label: "Display", official: true, forked: false, parent: "device", parent_id: uuidFor("t1") },
  { id: uuidFor("t3"), name: "amplifier", label: "Amplifier", official: true, forked: false, parent: "device", parent_id: uuidFor("t1") },
] as ComponentType[];

const product = (name: string, component_type: string, driver?: string, vendor?: string): Product =>
  ({ id: `p-${name}`, name, label: name.toUpperCase(), kind: "device", component_type, component_type_id: "x", driver, vendor, official: true }) as Product;

const driver = (name: string, inputs: Driver["spec"] extends infer S ? (S extends { inputs?: infer I } ? I : never) : never = [], spec = true): Driver =>
  ({ id: `d-${name}`, name, label: name, official: true, spec: spec ? { version: 1, transport: "snmp", inputs, polls: [{ name: "identity", schedule: { every: "30s" }, request: {}, emits: [] }] } : undefined }) as Driver;

const snmpInputs = [
  { name: "host", kind: "string" as const, required: true },
  { name: "port", kind: "number" as const, default: "161" },
  { name: "community", kind: "secret" as const, secret_type: "snmp-community", required: true },
];

describe("STEPS", () => {
  it("walks product, where, endpoint, credentials, test, done in that order", () => {
    expect(STEPS.map((s) => s.key)).toEqual(["product", "where", "endpoint", "credentials", "test", "done"]);
  });
});

describe("productOption", () => {
  it("labels a product and tells it apart by its type and vendor", () => {
    const byName = new Map(types.map((t) => [t.name, t]));
    expect(productOption(product("edge-55", "display", undefined, "boreal"), byName)).toEqual({ value: "edge-55", label: "EDGE-55", detail: "Display, boreal" });
    expect(productOption(product("thing", "unknown"), byName).detail).toBe("unknown");
  });
});

describe("driverChoices", () => {
  const products = [product("edge-55", "display", "edge-snmp"), product("edge-65", "display", "edge-http"), product("amp", "amplifier", "amp-snmp")];
  const drivers = [driver("amp-snmp"), driver("edge-http"), driver("edge-snmp"), driver("stub", [], false), driver("generic")];

  it("puts the product's own driver first, then the drivers its kin use, and never a stub", () => {
    const got = driverChoices(drivers, products, "edge-55", false);
    expect(got.map((c) => [c.driver.name, c.why])).toEqual([
      ["edge-snmp", "product"],
      ["edge-http", "kin"],
    ]);
  });

  it("adds every other attachable driver when asked for all", () => {
    const got = driverChoices(drivers, products, "edge-55", true);
    expect(got.map((c) => c.driver.name)).toEqual(["edge-snmp", "edge-http", "amp-snmp", "generic"]);
    expect(got.find((c) => c.driver.name === "stub")).toBeUndefined();
  });

  it("offers everything attachable when the product names no driver and has no kin", () => {
    expect(driverChoices(drivers, [product("lone", "display")], "lone", false).map((c) => c.why)).toEqual(["other", "other", "other", "other"]);
  });
});

describe("splitInputs", () => {
  it("puts the secret inputs on the credentials step and the rest on the endpoint step", () => {
    const s = splitInputs(driver("x", snmpInputs));
    expect(s.params.map((i) => i.name)).toEqual(["host", "port"]);
    expect(s.secrets.map((i) => i.name)).toEqual(["community"]);
  });
  it("is empty for no driver", () => {
    expect(splitInputs(undefined)).toEqual({ params: [], secrets: [] });
  });
});

describe("secretOptions", () => {
  it("offers only the secrets of the kind the input takes, named by their owner", () => {
    const secrets = [
      { id: uuidFor("s1"), name: "sim-community", label: "Sim community", secret_type: "snmp-community", owner_kind: "platform", fields: [] },
      { id: uuidFor("s2"), name: "room-community", secret_type: "snmp-community", owner_kind: "location", owner_name: "hq", fields: [] },
      { id: uuidFor("s3"), name: "login", secret_type: "http-basic", owner_kind: "platform", fields: [] },
    ];
    expect(secretOptions(secrets, "snmp-community")).toEqual([
      { value: "sim-community", label: "Sim community", detail: "sim-community, platform" },
      { value: "room-community", label: "room-community", detail: "room-community, location hq" },
    ]);
  });
});

describe("slotsFor", () => {
  const slot = (name: string, accepted: string[] | null, pinned: string[] | null = null) =>
    ({ name, label: name, accepted_types: accepted, pinned_products: pinned, assigned: 0, assigned_to: [], from_standard: true, impact: "outage", quorum: 1 }) as never;
  it("keeps the slots a product of this type may fill, an ancestor type included", () => {
    const got = slotsFor([slot("display", ["display"]), slot("any", null), slot("amp", ["amplifier"]), slot("dev", ["device"]), slot("pinned", null, ["other"])], product("edge-55", "display"), types);
    expect(got.map((s) => s.name)).toEqual(["display", "any", "dev"]);
  });
});

describe("inheritedLocation", () => {
  it("is the system's place, by uuid, until issue 878 makes the server inherit it", () => {
    expect(inheritedLocation([{ id: "sys", label: "Room", location_id: "loc-1" }] as never, "sys")).toBe("loc-1");
    expect(inheritedLocation([{ id: "sys", label: "Room" }] as never, "sys")).toBe("");
    expect(inheritedLocation([], "")).toBe("");
  });
});

describe("simChoice", () => {
  it("reads the driver attach a simulated device would get, address and inputs filled", () => {
    const plan = { outpost: "edge-hq", product: "edge-55", address: "127.60.0.9", endpoints: [{ driver: "edge-snmp", inputs: { host: "127.60.0.9", port: "161", community: "sim-community" } }] };
    expect(simChoice(plan)).toEqual({ outpost: "edge-hq", address: "127.60.0.9", driver: "edge-snmp", inputs: { host: "127.60.0.9", port: "161", community: "sim-community" } });
  });
  it("is nothing when the simulator can only be probed", () => {
    expect(simChoice({ outpost: "edge-hq", product: "x", address: "127.60.0.9", endpoints: [{ transport: "tcp", target: "127.60.0.9:80" }] })).toBeNull();
  });
});

describe("stepReady", () => {
  const d = (over: Partial<FlowDraft>): FlowDraft => ({ ...emptyDraft(), ...over });
  const edge = driver("edge-snmp", snmpInputs);
  it("needs a product to leave the first step", () => {
    expect(stepReady("product", d({}), undefined)).toBe(false);
    expect(stepReady("product", d({ product: "edge-55" }), undefined)).toBe(true);
  });
  it("needs a system before a slot", () => {
    expect(stepReady("where", d({}), undefined)).toBe(true);
    expect(stepReady("where", d({ slot: "display" }), undefined)).toBe(false);
  });
  it("needs a driver, an outpost and every required parameter on the endpoint step", () => {
    expect(stepReady("endpoint", d({ driver: "edge-snmp", outpost: "edge-hq" }), edge)).toBe(false);
    expect(stepReady("endpoint", d({ driver: "edge-snmp", outpost: "edge-hq", inputs: { host: "10.0.0.5" } }), edge)).toBe(true);
    expect(stepReady("endpoint", d({ driver: "edge-snmp", inputs: { host: "10.0.0.5" } }), edge)).toBe(false);
  });
  it("needs every required credential on the credentials step", () => {
    expect(stepReady("credentials", d({ inputs: { host: "h" } }), edge)).toBe(false);
    expect(stepReady("credentials", d({ inputs: { host: "h", community: "sim-community" } }), edge)).toBe(true);
  });
});

describe("failureStep", () => {
  it("sends a refused login back to the credentials", () => {
    expect(failureStep({ kind: "gated", title: "", reason: "auth: the agent answered authorizationError for the community" })).toBe("credentials");
    expect(failureStep({ kind: "failed", title: "", reason: "401 Unauthorized: the login was refused" })).toBe("credentials");
  });
  it("sends anything else back to the endpoint: the address, the port, the outpost", () => {
    expect(failureStep({ kind: "timeout", title: "", reason: "no answer" })).toBe("endpoint");
    expect(failureStep({ kind: "no-outpost", title: "", reason: "not connected" })).toBe("endpoint");
    expect(failureStep({ kind: "gated", title: "", reason: "unreachable" })).toBe("endpoint");
  });
});

function fakeIO(fail?: "component" | "slot" | "endpoint"): FlowIO & { log: string[] } {
  const log: string[] = [];
  return {
    log,
    createComponent: vi.fn(async (b) => {
      log.push(`create component ${b.product} system=${b.system ?? ""} location=${b.location ?? ""}`);
      if (fail === "component") throw new Error("refused");
      return { id: "c-1" };
    }),
    assignSlot: vi.fn(async (system, slot, component) => {
      log.push(`assign ${system}/${slot} ${component}`);
      if (fail === "slot") throw new Error("slot refused");
    }),
    createEndpoint: vi.fn(async (b) => {
      log.push(`create endpoint ${b.driver} on ${b.component} via ${b.outpost} ${JSON.stringify(b.inputs)}`);
      if (fail === "endpoint") throw new Error("endpoint refused");
      return { id: "e-1" };
    }),
    deleteEndpoint: vi.fn(async (id) => void log.push(`delete endpoint ${id}`)),
    deleteComponent: vi.fn(async (id) => void log.push(`delete component ${id}`)),
  };
}

const full: FlowDraft = {
  ...emptyDraft(),
  product: "edge-55",
  name: "",
  label: "Lobby display",
  expectedName: "display-3",
  system: "sys-1",
  slot: "display",
  location: "loc-1",
  outpost: "edge-hq",
  driver: "edge-snmp",
  inputs: { host: " 127.60.0.9 ", port: "", community: "sim-community" },
};

describe("commitFlow", () => {
  it("creates the component in its system and place, fills the slot, then attaches the driver", async () => {
    const io = fakeIO();
    const made = await commitFlow(full, io);
    expect(made).toEqual({ component: "c-1", endpoint: "e-1" });
    expect(io.log).toEqual([
      "create component edge-55 system=sys-1 location=loc-1",
      "assign sys-1/display c-1",
      'create endpoint edge-snmp on c-1 via edge-hq {"host":"127.60.0.9","community":"sim-community"}',
    ]);
    expect(io.createComponent).toHaveBeenCalledWith(expect.objectContaining({ name: undefined, expected_name: "display-3", label: "Lobby display" }));
  });

  it("takes the component back out when the endpoint is refused, so a failed commit leaves nothing", async () => {
    const io = fakeIO("endpoint");
    await expect(commitFlow(full, io)).rejects.toThrow("endpoint refused");
    expect(io.log.at(-1)).toBe("delete component c-1");
    expect(io.deleteEndpoint).not.toHaveBeenCalled();
  });

  it("takes the component back out when its slot is refused", async () => {
    const io = fakeIO("slot");
    await expect(commitFlow(full, io)).rejects.toThrow("slot refused");
    expect(io.log.at(-1)).toBe("delete component c-1");
    expect(io.createEndpoint).not.toHaveBeenCalled();
  });

  it("creates nothing more when the component itself is refused", async () => {
    const io = fakeIO("component");
    await expect(commitFlow(full, io)).rejects.toThrow("refused");
    expect(io.deleteComponent).not.toHaveBeenCalled();
  });
});

describe("undoCreated", () => {
  it("deletes the endpoint before its component, since a component with endpoints refuses its delete", async () => {
    const io = fakeIO();
    await undoCreated({ component: "c-1", endpoint: "e-1" }, io);
    expect(io.log).toEqual(["delete endpoint e-1", "delete component c-1"]);
  });
});
