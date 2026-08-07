import { PRODUCTS_KEY, listProducts } from "./products";
import { VENDORS_KEY, listVendors } from "./vendors";
import { DRIVERS_KEY, listDrivers } from "./drivers";
import { CAPABILITIES_KEY, listCapabilities } from "./capabilities";
import { STANDARDS_KEY, listStandards } from "./standards";
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
    copy: "A component points at a product (its shape), a product names its vendor, a driver is how we talk to it, and capabilities say what it can do.",
    entries: [
      { label: "Vendors", path: "/vendors", gate: ["vendor", "read"], key: VENDORS_KEY, list: listVendors },
      { label: "Products", path: "/products", gate: ["product", "read"], key: PRODUCTS_KEY, list: listProducts },
      { label: "Drivers", path: "/drivers", gate: ["driver", "read"], key: DRIVERS_KEY, list: listDrivers },
      { label: "Capabilities", path: "/capabilities", gate: ["capability", "read"], key: CAPABILITIES_KEY, list: listCapabilities },
      { label: "Templates", soon: true, issue: 615 },
    ],
  },
  {
    header: "Systems",
    copy: "A standard is the blueprint a system conforms to: the properties every conforming system exposes.",
    entries: [
      { label: "Standards", path: "/standards", gate: ["standard", "read"], key: STANDARDS_KEY, list: listStandards },
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
export function registryLock(
  row: { official?: boolean } | undefined,
  me: Me | null | undefined,
  resource: string,
): BladeLock {
  if (!row) return null;
  if (row.official) return OFFICIAL_LOCK;
  const edit = can(me, resource, "update") ? null : `Requires ${resource}:update`;
  const del = can(me, resource, "delete") ? null : `Requires ${resource}:delete`;
  return edit == null && del == null ? null : { edit, delete: del };
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
