import { describe, it, expect } from "vitest";
import {
  type Template,
  componentBody,
  kindChip,
  landingHref,
  needsNode,
  ofKinds,
  parseKinds,
  placeRef,
  placeTreeRows,
  placeValue,
  placesBody,
  quorumText,
  missingNeeds,
  permText,
  splitKindChip,
  standardBody,
  startNeeds,
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
    // The fleet's own standard of that name sets the counts when it holds one.
    expect(systemRoleSlots(sys, all, [{ name: "main-display", label: "Displays", quorum: 3 }, { name: "video-bar", label: "Bar", quorum: 1 }]).map((s) => [s.role, s.label, s.count])).toEqual([
      ["main-display", "Displays", 3],
      ["video-bar", "Bar", 1],
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

describe("what a start needs, mirroring the routes", () => {
  const holds = (...names: string[]) => (n: string) => names.includes(n);
  const ctx = { all, held: holds("mr55", "ds55") };
  const sysSet = [["system", "create"], ["system", "read"], ["system", "update"], ["component", "create"], ["component", "update"]];

  it("asks a standard for standard:create alone", () => {
    expect(startNeeds(std, ctx)).toEqual([["standard", "create"]]);
  });

  it("asks a component for update when it declares values, and endpoint:create when it attaches", () => {
    expect(startNeeds(comp, ctx)).toEqual([["component", "create"]]);
    const valued = { ...comp, component: { ...comp.component!, properties: { mounting: "wall" } } };
    expect(startNeeds(valued, { ...ctx, address: true })).toEqual([["component", "create"], ["component", "update"], ["endpoint", "create"]]);
  });

  it("asks a system for the system set, standard:create only when its standard would be created from its template", () => {
    expect(startNeeds(sys, ctx)).toEqual(sysSet);
    expect(startNeeds(sys, { ...ctx, held: holds() })).toEqual([...sysSet, ["standard", "create"]]);
    // The form's own choice of a held standard creates none.
    expect(startNeeds(sys, { ...ctx, held: holds("mine"), standard: "mine" })).toEqual(sysSet);
    expect(startNeeds(sys, { ...ctx, address: true })).toEqual([...sysSet, ["endpoint", "create"]]);
  });

  it("asks places for location:create, and the system set when the subtree carries systems", () => {
    // meeting-floor's systems are ds55-system (not in this catalog) and mr55-system (built to mr55, held).
    expect(startNeeds(place, ctx)).toEqual([["location", "create"], ...sysSet]);
    expect(startNeeds(place, { ...ctx, held: holds() })).toEqual([["location", "create"], ...sysSet, ["standard", "create"]]);
    const bare: Template = { kind: "place_template", name: "f", label: "F", place: { type: "floor" } };
    expect(startNeeds(bare, ctx)).toEqual([["location", "create"]]);
  });

  it("names what a deploy-shaped caller lacks", () => {
    const deploy = new Set(["location:create", "location:update", "system:create", "system:update", "component:create", "component:update", "system:read", "component:read"]);
    const allow = (r: string, a: string) => deploy.has(`${r}:${a}`);
    expect(missingNeeds(startNeeds(sys, ctx), allow)).toEqual([]);
    expect(permText(missingNeeds(startNeeds(sys, { ...ctx, held: holds(), address: true }), allow))).toBe("standard:create, endpoint:create");
    expect(permText(missingNeeds(startNeeds(std, ctx), allow))).toBe("standard:create");
  });
});

describe("what each start form posts", () => {
  it("creates a standard with only what the operator gave", () => {
    expect(standardBody("mr55", { name: " ", label: "", parent: "" })).toEqual({ template: "mr55", name: undefined, label: undefined, parent: undefined });
    expect(standardBody("mr55", { name: "mr55-east", label: "East rooms", parent: "meeting-room" })).toEqual({ template: "mr55", name: "mr55-east", label: "East rooms", parent: "meeting-room" });
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

});

describe("where a success lands", () => {
  it("carries the template's name as ?from= beside any other param", () => {
    expect(landingHref("/systems/abc", "mr55-system")).toBe("/systems/abc?from=mr55-system");
    expect(landingHref("/standards", "mr55", { open: "mr55-2" })).toBe("/standards?open=mr55-2&from=mr55");
  });
});
