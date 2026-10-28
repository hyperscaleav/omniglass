import { describe, it, expect } from "vitest";
import {
  type Template,
  componentBody,
  findCreated,
  forkBody,
  kindChip,
  landingHref,
  needsNode,
  ofKinds,
  parseKinds,
  placeId,
  placeRef,
  placeTreeRows,
  placeValue,
  placesBody,
  quorumText,
  splitKindChip,
  systemBody,
  systemRoleSlots,
  templateSummary,
  templatesHref,
} from "./templates";
import { uuidFor } from "./testids";

// The template catalog's pure rules (#1329): which kinds an address asks for,
// what each kind's body says it creates, what each start form posts, and where
// a success lands. The page and its forms are thin over these.

const std: Template = {
  kind: "standard_template",
  name: "mr55",
  label: "Meeting Room 55",
  standard: {
    parent: "meeting-room",
    roles: [
      { name: "video-bar", label: "Video bar", quorum: 1, capacity: 1 },
      { name: "main-display", label: "Main display", quorum: 2, accepted_types: ["display"], pinned_products: ["boreal-edge-55"] },
    ],
  },
};
const comp: Template = { kind: "component_template", name: "boreal-edge-55", label: "Boreal Edge 55 display", component: { product: "boreal-edge-55", inputs: { port: "161" } } };
const sys: Template = {
  kind: "system_template",
  name: "mr55-system",
  label: "Meeting Room 55, staffed",
  system: { standard: "mr55", roles: { "main-display": { component: "boreal-edge-55" }, "video-bar": { product: "kestrel-vroom" } } },
};
const place: Template = {
  kind: "place_template",
  name: "meeting-floor",
  label: "A floor of meeting rooms",
  place: {
    type: "floor",
    label: "Meeting floor",
    systems: [{ template: "ds55-system", label: "Lift lobby sign" }],
    places: [
      { type: "room", name: "mr-1", label: "Meeting room 1", systems: [{ template: "mr55-system" }] },
      { type: "room", name: "mr-2", systems: [{ template: "mr55-system" }] },
    ],
  },
};
const all = [std, sys, place, comp];

describe("which kinds an address asks for", () => {
  it("reads a comma list in catalog order, dropping unknown words", () => {
    expect(parseKinds("system_template,standard_template,room_template")).toEqual(["standard_template", "system_template"]);
    expect(parseKinds(undefined)).toEqual([]);
    expect(parseKinds(["place_template"])).toEqual(["place_template"]);
  });

  it("builds the filtered address with the place a create flow carried", () => {
    expect(templatesHref(["standard_template", "system_template"])).toBe("/templates?kind=standard_template,system_template");
    expect(templatesHref(["place_template"], "loc-uuid")).toBe("/templates?kind=place_template&place=loc-uuid");
    expect(templatesHref([])).toBe("/templates");
  });

  it("keeps the kind chip in the URL and every other chip on the page", () => {
    expect(kindChip(["system_template"])).toEqual([{ key: "kind", op: "eq", values: ["system_template"] }]);
    expect(kindChip([])).toEqual([]);
    const name = { key: "name", op: "contains" as const, values: ["mr"] };
    const notKind = { key: "kind", op: "neq" as const, values: ["place_template"] };
    expect(splitKindChip([{ key: "kind", op: "eq", values: ["system_template", "standard_template", "bogus"] }, name, notKind])).toEqual({
      kinds: ["standard_template", "system_template"],
      rest: [name, notKind],
    });
  });

  it("filters rows to the kinds asked, every row when none is", () => {
    expect(ofKinds(all, ["component_template"]).map((t) => t.name)).toEqual(["boreal-edge-55"]);
    expect(ofKinds(all, [])).toHaveLength(4);
  });
});

describe("what a template creates", () => {
  it("summarizes each kind from its own body", () => {
    expect(templateSummary(std)).toBe("2 roles, a variant of meeting-room");
    expect(templateSummary(sys)).toBe("built to mr55, 2 components");
    expect(templateSummary(place)).toBe("3 places, 3 systems");
    expect(templateSummary(comp)).toBe("boreal-edge-55");
  });

  it("flattens a place subtree in pre-order: a place, its systems, then the places beneath", () => {
    expect(placeTreeRows(place.place!).map((r) => [r.kind, r.depth, r.kind === "place" ? r.type : r.template])).toEqual([
      ["place", 0, "floor"],
      ["system", 1, "ds55-system"],
      ["place", 1, "room"],
      ["system", 2, "mr55-system"],
      ["place", 1, "room"],
      ["system", 2, "mr55-system"],
    ]);
  });

  it("staffs a system template's roles to the quorum its standard template declares, in declared order", () => {
    expect(systemRoleSlots(sys, all)).toEqual([
      { role: "video-bar", label: "Video bar", count: 1, fill: "kestrel-vroom" },
      { role: "main-display", label: "Main display", count: 2, fill: "Boreal Edge 55 display" },
    ]);
    // A standard the catalog does not hold staffs one per role, by its name.
    expect(systemRoleSlots({ ...sys, system: { ...sys.system!, standard: "absent" } }, all).map((s) => s.count)).toEqual([1, 1]);
  });

  it("reads a role's quorum as exact, a range, or a floor", () => {
    expect(quorumText({ name: "a", label: "", quorum: 1, capacity: 1 })).toBe("1");
    expect(quorumText({ name: "a", label: "", quorum: 1, capacity: 3 })).toBe("1 to 3");
    expect(quorumText({ name: "a", label: "", quorum: 2 })).toBe("2 or more");
  });
});

describe("what each start form posts", () => {
  it("forks with only what the operator gave", () => {
    expect(forkBody("mr55", { name: " ", label: "", parent: "" })).toEqual({ template: "mr55", name: undefined, label: undefined, parent: undefined });
    expect(forkBody("mr55", { name: "mr55-east", label: "East rooms", parent: "meeting-room" })).toEqual({ template: "mr55", name: "mr55-east", label: "East rooms", parent: "meeting-room" });
  });

  it("puts the address on the host input and a secret by its input name", () => {
    expect(componentBody("boreal-edge-55", { place: "p1", name: "", label: "Lobby", node: "n1", address: " 10.0.0.9 ", secrets: { community: "snmp-ro", other: "" } })).toEqual({
      template: "boreal-edge-55", location: "p1", name: undefined, label: "Lobby", node: "n1", inputs: { host: "10.0.0.9", community: "snmp-ro" },
    });
    expect(componentBody("x", { place: "", name: "", label: "", node: "", address: "", secrets: {} }).inputs).toBeUndefined();
  });

  it("attaches a system's roles in slot order up to the last address given", () => {
    const body = systemBody("mr55-system", {
      place: "p1", name: "", label: "", standard: "", node: "n1",
      addresses: { "main-display": ["", "10.0.0.2"], "video-bar": [""] },
    });
    expect(body.attach).toEqual({ "main-display": [{}, { host: "10.0.0.2" }] });
    expect(body.standard).toBeUndefined();
    expect(systemBody("s", { place: "", name: "", label: "", standard: "mine", node: "", addresses: {} })).toMatchObject({ standard: "mine", attach: undefined });
  });

  it("starts places under a parent, or as a new top", () => {
    expect(placesBody("meeting-floor", { parent: "", name: "level-9", label: "" })).toEqual({ template: "meeting-floor", parent: undefined, name: "level-9", label: undefined });
  });

  it("asks for a node exactly when an address is given", () => {
    expect(needsNode({ node: "", address: "10.0.0.1" })).toBe(true);
    expect(needsNode({ node: "n1", address: "10.0.0.1" })).toBe(false);
    expect(needsNode({ node: "", addresses: { a: ["", " "] } })).toBe(false);
    expect(needsNode({ node: "", addresses: { a: ["", "h"] } })).toBe(true);
  });
});

describe("which place", () => {
  const rows = [
    { id: uuidFor("l-hq"), name: "hq", path: "hq" },
    { id: uuidFor("l-west"), name: "west", path: "hq.west" },
    { id: uuidFor("l-new"), name: "fresh" },
  ];

  it("offers a place by its dotted path, a row not yet pathed by its uuid", () => {
    expect(placeValue(rows[1])).toBe("hq.west");
    expect(placeValue(rows[2])).toBe(uuidFor("l-new"));
  });

  it("resolves a carried uuid, path or unique name to the offered value", () => {
    expect(placeRef(rows, uuidFor("l-west"))).toBe("hq.west");
    expect(placeRef(rows, "hq.west")).toBe("hq.west");
    expect(placeRef(rows, "west")).toBe("hq.west");
    expect(placeRef(rows, undefined)).toBe("");
  });

  it("resolves the offered value back to the uuid a placement compares on", () => {
    expect(placeId(rows, "hq.west")).toBe(uuidFor("l-west"));
    expect(placeId(rows, "")).toBeUndefined();
  });
});

describe("where a success lands", () => {
  it("carries the template's name as ?from= beside any other param", () => {
    expect(landingHref("/systems/abc", "mr55-system")).toBe("/systems/abc?from=mr55-system");
    expect(landingHref("/standards", "mr55", { open: "mr55-2" })).toBe("/standards?open=mr55-2&from=mr55");
  });

  it("tells two same-named rows apart by where they were placed", () => {
    const rows = [
      { id: uuidFor("a"), name: "meeting-1", at: "p1" },
      { id: uuidFor("b"), name: "meeting-1", at: "p2" },
      { id: uuidFor("c"), name: "sign-1", at: undefined },
    ];
    expect(findCreated(rows, "meeting-1", (r) => r.at, "p2")?.id).toBe(uuidFor("b"));
    expect(findCreated(rows, "sign-1", (r) => r.at, "p9")?.id).toBe(uuidFor("c"));
    expect(findCreated(rows, "nothing", (r) => r.at, undefined)).toBeUndefined();
  });
});
