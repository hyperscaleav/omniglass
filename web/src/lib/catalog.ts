import { PRODUCTS_KEY, listProducts } from "./products";
import { COMPONENT_TYPES_KEY, listComponentTypes } from "./component_types";
import { VENDORS_KEY, listVendors } from "./vendors";
import { DRIVERS_KEY, listDrivers } from "./drivers";
import { STANDARDS_KEY, listStandards } from "./standards";
import { SYSTEM_TYPES_KEY, listSystemTypes } from "./system_types";
import { LOCATION_TYPES_KEY, listLocationTypes } from "./location_types";
import { METRICS_KEY, listMetricTypes } from "./metric_types";
import { PROPERTIES_KEY, listProperties } from "./properties";
import { EVENT_TYPES_KEY, listEventTypes } from "./event_types";
import { COMMAND_TYPES_KEY, listCommandTypes } from "./command_types";
import { TAGS_KEY, listTags } from "./tags";
import { type Me, can } from "./auth";
import { type BladeLock } from "./blades";

// The Catalog group table: the single source of truth for the catalog area's
// IA. The CatalogShell subrail renders it as navigation (one link per entry,
// each opening the registry's own routed page in the shell's pane) and the
// /catalog Overview renders it as section cards, so membership, order, labels,
// teaching copy, gates, and count sources can never drift between the two
// surfaces. Each live entry carries the SAME query key and fetch its list page
// uses, so a count on either surface and the page's own list are one cache
// entry: navigating from a count refetches nothing.
//
// Deliberate absences: no Logs entry under Telemetry (no log_type registry
// exists), and no Secret types entry anywhere (the standing ruling: it holds
// no nav slot; /secret-types stays routed and renders inside the shell's pane,
// reachable by URL under its secret:read gate).

export type CatalogEntry = {
  label: string;
  // The canonical route this entry opens. A soon entry with a path links to
  // its routed stub page; a soon entry without one only reserves the slot.
  path?: string;
  // Pending: the registry does not exist yet. Wears the rail's SOON treatment.
  soon?: boolean;
  // The pending registry's tracking issue, nav.ts's `issue` pattern: a routed
  // slot mirrors its nav.ts off-rail entry, whose stub page (SectionStub via
  // navByPath) shows the same number; a pathless slot carries it here only.
  issue?: number;
  // The permission tokens reading this registry requires (the route guard's
  // vocabulary); absent means ungated. A caller failing the gate loses the
  // entry, and with it the entry's count query (never created).
  gate?: string[];
  // The registry's shared list-query key and fetch, the exact pair its list
  // page uses (one cache, one failure domain per registry).
  key?: readonly string[];
  list?: () => Promise<readonly unknown[]>;
};

export type CatalogGroup = {
  header: string;
  // The teaching sentence on the group's Overview card.
  copy: string;
  entries: CatalogEntry[];
};

// Each soon slot names its tracking issue (the no-bare-TODO rule): the
// template registries are #615 (component), #616 (system), #617 (location);
// notifications is #618, rules is #624 (its vocabulary is fenced separately
// in #606). Group copy follows the direction axis: Telemetry is what you
// receive, Actions is what you send or run.
export const CATALOG_GROUPS: CatalogGroup[] = [
  {
    header: "Telemetry",
    copy: "What you receive, typed by lane: metrics (quantities), properties (values), events (happenings).",
    entries: [
      { label: "Metrics", path: "/metrics", gate: ["metric_type", "read"], key: METRICS_KEY, list: listMetricTypes },
      { label: "Properties", path: "/properties", gate: ["property_type", "read"], key: PROPERTIES_KEY, list: listProperties },
      { label: "Events", path: "/event-types", gate: ["event_type", "read"], key: EVENT_TYPES_KEY, list: listEventTypes },
    ],
  },
  {
    header: "Actions",
    copy: "What you send or run: commands (the instructions you issue, with a target and a settle window), rules (soon), notifications (soon).",
    entries: [
      { label: "Commands", path: "/command-types", gate: ["command_type", "read"], key: COMMAND_TYPES_KEY, list: listCommandTypes },
      { label: "Rules", path: "/rules", soon: true, issue: 624 },
      { label: "Notifications", path: "/notifications", soon: true, issue: 618 },
    ],
  },
  {
    header: "Components",
    copy: "A component points at a product (its shape), a product names its vendor, a driver is how we talk to it, and a component type is the device-class genus it is classified under.",
    entries: [
      { label: "Vendors", path: "/vendors", gate: ["vendor", "read"], key: VENDORS_KEY, list: listVendors },
      { label: "Products", path: "/products", gate: ["product", "read"], key: PRODUCTS_KEY, list: listProducts },
      { label: "Drivers", path: "/drivers", gate: ["driver", "read"], key: DRIVERS_KEY, list: listDrivers },
      // "Types": the Components header already says where it lives (mirrors
      // Locations' own Types entry below). The device-class genus registry
      // (ADR-0085): a product is classified under one of its nodes.
      { label: "Types", path: "/component-types", gate: ["component_type", "read"], key: COMPONENT_TYPES_KEY, list: listComponentTypes },
      { label: "Templates", soon: true, issue: 615 },
    ],
  },
  {
    header: "Systems",
    copy: "A system type is the coarse kind of space a system is (a boardroom, a classroom, a video wall); a standard is the blueprint it conforms to, the properties every conforming system exposes.",
    entries: [
      { label: "Standards", path: "/standards", gate: ["standard", "read"], key: STANDARDS_KEY, list: listStandards },
      // "Types": the Systems header already says where it lives (mirrors the
      // Components and Locations groups). The coarse space taxonomy
      // (ADR-0096): a system is classified as one of its nodes.
      { label: "Types", path: "/system-types", gate: ["system_type", "read"], key: SYSTEM_TYPES_KEY, list: listSystemTypes },
      { label: "Templates", soon: true, issue: 616 },
    ],
  },
  {
    header: "Locations",
    copy: "A location type classifies a place (campus, building, floor, room) and carries its contract.",
    entries: [
      // "Types": the Locations header already says where it lives.
      { label: "Types", path: "/location-types", gate: ["location_type", "read"], key: LOCATION_TYPES_KEY, list: listLocationTypes },
      { label: "Templates", soon: true, issue: 617 },
    ],
  },
  {
    header: "Metadata",
    copy: "Tags: the governed key vocabulary the whole estate labels with.",
    entries: [{ label: "Tags", path: "/tags", gate: ["tag", "read"], key: TAGS_KEY, list: listTags }],
  },
];

// The registries' shared official-row sentence, formerly each blade's in-body
// banner, now the lock reason on the greyed Edit / Delete pair. Operator copy
// says official, never seed: the seeding mechanics are not operator vocabulary.
export const OFFICIAL_LOCK = "Official: ships with Omniglass and updates with it.";

// The ROOT-STEM rule, in the words the two tree registries' create forms say it
// with. It is one rule (`ErrRootComponentTypeNeedsStem` and
// `ErrRootSystemTypeNeedsStem` refuse the same thing on their own tier: a root
// has no ancestor to take a stem from), and it was written twice, so one copy
// could go missing without the other noticing. It did: the component form never
// carried it, and an operator creating a root component type met the constraint
// as a 422 after submitting while the same operator creating a root system type
// had been told (#744). One sentence in one place is what stops that recurring;
// the FACT each hint leads with still differs, because a component's stem and a
// system's are different facts.
export const ROOT_STEM_HINT = "Leave blank to inherit the parent's; required on a root.";

// The PARENT picker's sentence on both tree registries, for the same reason:
// placement is chosen once (neither gateway has a reparent leg) and choosing
// root is what makes the stem above mandatory, so the two statements have to
// agree and are therefore written once.
export const TYPE_PARENT_HINT =
  "Where this type grafts in the tree. Root creates a new top-level genus and then needs a stem of its own; the gateway has no reparent leg, so choose carefully.";

// registryLock is the catalog blades' one read-only verdict: the value the
// edit slot's `locked` binding carries, one voice on every registry page. An
// official row wears the OFFICIAL_LOCK string for everyone, owner included:
// both buttons greyed with the one sentence. A custom row locks per verb: each
// side of the footer pair greys only when the caller lacks THAT side's
// permission, named exactly (`Requires <resource>:update` on Edit, `Requires
// <resource>:delete` on Delete), so a delete-only caller sees a live Delete
// beside a greyed Edit and vice versa, and a greyed button never names a
// permission that would not unlock it. Both verbs held collapses to null (no
// lock; the body's live bindings render untouched). A row that has not
// resolved locks nothing; a row with no official flag (a tag) is judged on
// the permission arm alone.
//
// `forkable` is the opt-in for a registry that has adopted the fork (#655,
// ADR-0095): an edit there does not write the shipped row, it stores the
// operator's version over it, so Edit is live on a shipped row and judged on
// the update permission like any other. Delete stays locked with the official
// sentence, because a shipped row still cannot be deleted (restoring, which
// discards the operator's version, is the footer's destructive action
// instead). Default false: the registries that have not adopted yet keep the
// flat read-only verdict, which is still the truth for them.
export function registryLock(
  row: { official?: boolean; forked?: boolean } | undefined,
  me: Me | null | undefined,
  resource: string,
  opts?: { forkable?: boolean },
): BladeLock {
  if (!row) return null;
  const edit = can(me, resource, "update") ? null : `Requires ${resource}:update`;
  if (row.official) {
    if (!opts?.forkable) return OFFICIAL_LOCK;
    // A shipped row is never deleted, so the destructive slot is greyed with
    // the official sentence while the row is still pristine: there is neither
    // a row to remove nor changes to discard. Once forked it unlocks, because
    // the page puts Restore (discard the fork) in that slot.
    return { edit, delete: row.forked ? null : OFFICIAL_LOCK };
  }
  const del = can(me, resource, "delete") ? null : `Requires ${resource}:delete`;
  return edit == null && del == null ? null : { edit, delete: del };
}

// registryOrigin is the three-state provenance a fork-adopting registry shows:
// a row this release ships, a row the operator made, or a shipped row the
// operator has overridden. Two booleans on the wire, one word on the page.
export function registryOrigin(row: { official?: boolean; forked?: boolean }): "shipped" | "yours" | "overridden" {
  if (!row.official) return "yours";
  return row.forked ? "overridden" : "shipped";
}

// The Overview landing's route, the subrail's first entry.
export const OVERVIEW_PATH = "/catalog";

// visibleGroups filters the table to what a caller may see, the same shape as
// nav.ts's filterNav: a gated entry is kept only when `allow` passes its
// tokens (an ungated or soon entry is always kept), and a group left with no
// entries drops entirely, header or card included. Both catalog surfaces judge
// visibility through this one function over the same can() the rail uses.
export function visibleGroups(allow: (tokens: string[]) => boolean): CatalogGroup[] {
  const out: CatalogGroup[] = [];
  for (const g of CATALOG_GROUPS) {
    const entries = g.entries.filter((e) => !e.gate || allow(e.gate));
    if (entries.length) out.push({ ...g, entries });
  }
  return out;
}

// The routed soon slots' paths (/rules, /notifications): the router mounts
// SectionStub for each INSIDE the shell, so the stub renders in the pane, and
// index.tsx keeps them out of the top-level stub loop.
export const CATALOG_STUB_PATHS: string[] = CATALOG_GROUPS.flatMap((g) =>
  g.entries.filter((e) => e.soon && e.path).map((e) => e.path!),
);

// InheritedFacts is what the type registries' LISTING serves beside the raw
// fields (#716's console half, extending #695's `resolved_icon`): for each fact
// a row can inherit, the value it would take if it stated none, and the name of
// the ancestor that value comes from.
//
// It is a different question from `resolved_icon`, which answers what a row
// SHOWS and is therefore the row's own value on every row that states one. A
// blade's placeholder needs "clear this box and you get what?", so it needs
// this one; using the shown value would print the string the operator had just
// deleted back at them.
//
// The source is per FACT rather than per row, because one type can take its
// stem from a grandparent and its abbrev from its parent, and it is served
// rather than derived, because deriving it means climbing the type chain in
// TypeScript, which is what #695 deleted and #702 and #710 both refused to
// bring back.
export type InheritedFacts = {
  inherited_stem?: string;
  inherited_stem_source?: string;
  inherited_icon?: string;
  inherited_icon_source?: string;
  inherited_abbrev?: string;
  inherited_abbrev_source?: string;
};

// InheritedFact is one of them, in the shape a field consumes. Both halves are
// absent together: a chain that states the fact nowhere has no value to offer
// and no ancestor to name.
export type InheritedFact = { value: string; from: string };

// inheritedFact reads one fact off a served row. An undefined row (still
// loading, or not found) inherits nothing, which is the same answer a root
// gives and renders identically.
export function inheritedFact(row: InheritedFacts | undefined, fact: "stem" | "icon" | "abbrev"): InheritedFact {
  if (!row) return { value: "", from: "" };
  switch (fact) {
    case "stem":
      return { value: row.inherited_stem ?? "", from: row.inherited_stem_source ?? "" };
    case "icon":
      return { value: row.inherited_icon ?? "", from: row.inherited_icon_source ?? "" };
    default:
      return { value: row.inherited_abbrev ?? "", from: row.inherited_abbrev_source ?? "" };
  }
}

// pickInheritedFacts copies the served pairs off a wire row, the one place the
// six field names are spelled for both registries' data layers.
export function pickInheritedFacts(t: InheritedFacts): InheritedFacts {
  return {
    inherited_stem: t.inherited_stem,
    inherited_stem_source: t.inherited_stem_source,
    inherited_icon: t.inherited_icon,
    inherited_icon_source: t.inherited_icon_source,
    inherited_abbrev: t.inherited_abbrev,
    inherited_abbrev_source: t.inherited_abbrev_source,
  };
}
