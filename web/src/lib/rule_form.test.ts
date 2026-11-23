import { describe, it, expect } from "vitest";
import { uuidFor } from "./testids";
import { componentOptions, humanizeName, readsKeyFromParam, readsOptions, ruleFormPrefill, alarmOnThisHref } from "./rule_form";

const metrics = [
  { name: "temperature-c", label: "Temperature", unit: "°C", data_type: "float", official: true },
  { name: "latency-ms", unit: "ms", data_type: "float", official: true },
];
const properties = [
  { name: "firmware", label: "Firmware", data_type: "string", official: true },
  { name: "endpoint-reachable", data_type: "bool", official: true },
];

describe("readsOptions", () => {
  it("groups metrics and properties, each showing label, name and unit", () => {
    const o = readsOptions({ metrics, properties });
    expect(o.filter((x) => x.group === "Metrics").map((x) => x.value)).toEqual(["metric:latency-ms", "metric:temperature-c"]);
    expect(o.filter((x) => x.group === "Properties").map((x) => x.value)).toEqual(["property:endpoint-reachable", "property:firmware"]);
    const t = o.find((x) => x.value === "metric:temperature-c")!;
    expect(t.label).toBe("Temperature");
    expect(t.detail).toBe("temperature-c · °C");
    expect(o.find((x) => x.value === "property:firmware")!.detail).toBe("firmware");
  });

  it("lists the component's own metrics and properties first, once each", () => {
    const o = readsOptions({ metrics, properties, onComponent: { label: "Display 1", keys: ["property:firmware", "metric:temperature-c"] } });
    expect(o.slice(0, 2).map((x) => [x.value, x.group])).toEqual([
      ["metric:temperature-c", "On Display 1"],
      ["property:firmware", "On Display 1"],
    ]);
    expect(o.filter((x) => x.value === "metric:temperature-c")).toHaveLength(1);
    expect(o.filter((x) => x.group === "Metrics").map((x) => x.value)).toEqual(["metric:latency-ms"]);
  });
});

describe("componentOptions", () => {
  it("tells same-named components apart by their place", () => {
    const o = componentOptions([
      { id: uuidFor("a"), name: "display-1", label: "Display 1", location: "Room A", system: "Sys A" },
      { id: uuidFor("b"), name: "display-1", label: "Display 1", location: "Room B" },
      { id: uuidFor("c"), name: "orphan" },
    ]);
    expect(o.map((x) => [x.value, x.label, x.detail])).toEqual([
      [uuidFor("a"), "Display 1", "Room A · Sys A"],
      [uuidFor("b"), "Display 1", "Room B"],
      [uuidFor("c"), "orphan", "unplaced"],
    ]);
  });
});

describe("humanizeName", () => {
  it("reads a kebab name as words and leaves acronyms as typed", () => {
    expect(humanizeName("catalog-cpu-hot")).toBe("Catalog cpu hot");
    expect(humanizeName("hq-DSP-low")).toBe("Hq DSP low");
    expect(humanizeName("")).toBe("");
  });
});

describe("prefill from the URL", () => {
  it("resolves a bare key to its lane through the catalogs", () => {
    expect(readsKeyFromParam("temperature-c", { metrics, properties })).toBe("metric:temperature-c");
    expect(readsKeyFromParam("firmware", { metrics, properties })).toBe("property:firmware");
    expect(readsKeyFromParam("nope", { metrics, properties })).toBeUndefined();
  });

  it("reads reads and component params, ignoring blanks", () => {
    expect(ruleFormPrefill({ new: "1", reads: "temperature-c", component: "abc" })).toEqual({ reads: "temperature-c", component: "abc" });
    expect(ruleFormPrefill({ new: "1" })).toEqual({});
    expect(ruleFormPrefill({ reads: ["x", "y"], component: "" })).toEqual({ reads: "x" });
  });

  it("builds the Alarm on this link", () => {
    expect(alarmOnThisHref("temperature-c", "c-1")).toBe("/event-rules?new=1&reads=temperature-c&component=c-1");
  });
});
