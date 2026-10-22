import { describe, it, expect } from "vitest";
import {
  type CalcRule,
  aliasError,
  calcTemplates,
  createCalcBody,
  draftFromCalcRule,
  emptyCalcDraft,
  hasErrors,
  liveWords,
  previewCalcBody,
  suggestAlias,
  updateCalcBody,
  validateCalcDraft,
  valueTypeOf,
  withInputKey,
} from "./calc_rules";

const rule: CalcRule = {
  id: "11111111-1111-7111-8111-111111111111", name: "room-mean", owner_kind: "system",
  selector: { system_types: ["meeting"] },
  inputs: [{ alias: "t", key: "room-temperature", key_id: "22222222-2222-7222-8222-222222222222", lane: "metric" }],
  reduce: { expr: "avg(t)" },
  output_key: "avg-temp", output_lane: "metric", enabled: true, official: false, version: 2,
  created_at: "2026-10-21T00:00:00Z", updated_at: "2026-10-21T00:00:00Z",
  owners_computed: 3, owners_pending: 1, owners_unsupported: 0, owners_failed: 0,
};

describe("calc rules", () => {
  it("reads a key's value type and the live counts in words", () => {
    expect(valueTypeOf("metric")).toBe("number");
    expect(valueTypeOf("property", "bool")).toBe("bool");
    expect(valueTypeOf("property", "string")).toBe("text");
    expect(liveWords(rule)).toBe("3 computed, 1 pending");
    expect(liveWords({ ...rule, owners_computed: 0, owners_pending: 0 })).toBe("nothing computed yet");
  });

  // The alias rules the server holds (#1018), mirrored so the form refuses
  // what a save would: an identifier of at most 64, not a reserved name (a
  // keyword, a builtin or list function, the site's own bindings), unique.
  it("judges an alias the way the server does", () => {
    for (const ok of ["tcp", "tcp_connect_time", "_x", "Temp2", "a".repeat(64)]) expect(aliasError(ok, [])).toBeUndefined();
    expect(aliasError("", [])).toMatch(/alias/);
    expect(aliasError("2x", [])).toMatch(/letter or underscore/);
    expect(aliasError("in-call", [])).toMatch(/letter or underscore/);
    expect(aliasError("a".repeat(65), [])).toMatch(/64/);
    for (const reserved of ["true", "let", "len", "max", "avg", "majority", "count", "members", "component", "system", "value", "has"]) {
      expect(aliasError(reserved, [])).toMatch(/reserved/);
    }
    expect(aliasError("t", ["t"])).toMatch(/two inputs/);
  });

  // The alias a key suggests is its name in snake_case, made valid: a
  // leading digit prefixed with _, a reserved or taken name suffixed.
  it("suggests an alias from a key's name", () => {
    expect(suggestAlias("tcp-connect-time", [])).toBe("tcp_connect_time");
    expect(suggestAlias("room.temperature", [])).toBe("room_temperature");
    expect(suggestAlias("inCall", [])).toBe("in_call");
    expect(suggestAlias("HDMI-Input", [])).toBe("hdmi_input");
    expect(suggestAlias("2nd-display", [])).toBe("_2nd_display");
    expect(suggestAlias("--", [])).toBe("input");
    expect(suggestAlias("count", [])).toBe("count_2");
    expect(suggestAlias("temp", ["temp"])).toBe("temp_2");
    expect(suggestAlias("temp", ["temp", "temp_2"])).toBe("temp_3");
    const long = suggestAlias("x".repeat(80), []);
    expect(long.length).toBeLessThanOrEqual(64);
    expect(aliasError(long, [])).toBeUndefined();
  });

  // Choosing a key fills an input's alias from it until the author edits
  // the alias; an edited alias stays put when the key changes.
  it("pre-fills an input's alias from its key until the alias is edited", () => {
    const inputs = [{ alias: "t", key: "room-temperature", aliasEdited: true }, { alias: "", key: "", aliasEdited: false }];
    const next = withInputKey(inputs, 1, "tcp-connect-time");
    expect(next[1]).toEqual({ alias: "tcp_connect_time", key: "tcp-connect-time", aliasEdited: false });
    expect(withInputKey(next, 1, "occupancy-count")[1].alias).toBe("occupancy_count");
    expect(withInputKey(next, 0, "uptime")[0]).toEqual({ alias: "t", key: "uptime", aliasEdited: true });
    // A suggestion never repeats another input's alias.
    const clash = withInputKey([{ alias: "temp", key: "temp-a", aliasEdited: true }, { alias: "", key: "", aliasEdited: false }], 1, "temp");
    expect(clash[1].alias).toBe("temp_2");
  });

  it("judges a draft the way the server will", () => {
    const d = emptyCalcDraft();
    const e = validateCalcDraft(d);
    expect(e.name).toBeTruthy();
    expect(e.input?.[0]).toBeTruthy();
    expect(e.reduce).toBeTruthy();
    expect(e.outputKey).toBeTruthy();
    const ok = { ...d, name: "room-mean", inputs: [{ alias: "t", key: "room-temperature", aliasEdited: false }], expr: "avg(t)", outputKey: "avg-temp" };
    expect(hasErrors(validateCalcDraft(ok))).toBe(false);
    expect(validateCalcDraft({ ...ok, inputs: [{ alias: "in-call", key: "in-call", aliasEdited: true }] }).input?.[0]).toMatch(/letter or underscore/);
    expect(validateCalcDraft({ ...ok, inputs: [{ alias: "members", key: "in-call", aliasEdited: true }] }).input?.[0]).toMatch(/reserved/);
    expect(validateCalcDraft({
      ...ok, inputs: [{ alias: "a", key: "in-call", aliasEdited: true }, { alias: "a", key: "video-input", aliasEdited: true }],
    }).input?.[1]).toMatch(/two inputs/);
    expect(validateCalcDraft({
      ...ok, inputs: [{ alias: "a", key: "in-call", aliasEdited: true }, { alias: "b", key: "in-call", aliasEdited: true }],
    }).input?.[1]).toMatch(/read twice/);
    expect(validateCalcDraft({ ...ok, inputs: [{ alias: "t", key: "", aliasEdited: true }] }).input?.[0]).toMatch(/key/);
    expect(validateCalcDraft({ ...ok, expr: " " }).reduce).toBeTruthy();
    // An edit keeps a stored name it would not accept anew.
    expect(validateCalcDraft({ ...ok, name: "Old_Name" }, "Old_Name").name).toBeUndefined();
  });

  it("sends a create body, an edit's changes only, and a preview candidate", () => {
    const d = draftFromCalcRule(rule);
    expect(d.inputs).toEqual([{ alias: "t", key: "room-temperature", aliasEdited: true }]);
    expect(createCalcBody({ ...d, name: "copy" })).toEqual({
      name: "copy", owner_kind: "system", selector: { system_types: ["meeting"], systems: [] },
      inputs: [{ alias: "t", key: "room-temperature" }], reduce: { expr: "avg(t)" }, output_key: "avg-temp", enabled: true,
    });
    expect(updateCalcBody(d, rule)).toEqual({});
    expect(updateCalcBody({ ...d, expr: "len(members) * 1.0" }, rule)).toEqual({ reduce: { expr: "len(members) * 1.0" } });
    expect(updateCalcBody({ ...d, inputs: [{ alias: "temp", key: "room-temperature", aliasEdited: true }], expr: "avg(temp)" }, rule)).toEqual({
      inputs: [{ alias: "temp", key: "room-temperature" }], reduce: { expr: "avg(temp)" },
    });
    expect(updateCalcBody({ ...d, enabled: false, systems: ["sys-1"] }, rule)).toEqual({
      enabled: false, selector: { system_types: ["meeting"], systems: ["sys-1"] },
    });
    expect(previewCalcBody(d, "room-mean")).toMatchObject({ name: "room-mean", owner_kind: "system", output_key: "avg-temp", reduce: { expr: "avg(t)" } });
    expect(previewCalcBody(d).name).toBeUndefined();
  });

  // The start-from templates: each list function with its slot, the cursor
  // placed in the slot, and on a system the count over members.
  it("offers start-from templates with the cursor in the slot", () => {
    const comp = calcTemplates("component");
    expect(comp.map((t) => t.text)).toEqual(["max()", "min()", "avg()", "sum()", "len()", "any()", "all()", "majority()"]);
    expect(comp[0].cursor).toBe(4);
    const sys = calcTemplates("system");
    const count = sys.find((t) => t.text.startsWith("count"))!;
    expect(count.text).toBe("count(members, )");
    expect(count.text.slice(0, count.cursor)).toBe("count(members, ");
    for (const t of [...comp, ...sys]) expect(t.label).toBe(t.text.slice(0, t.cursor) + "▮" + t.text.slice(t.cursor));
  });
});
