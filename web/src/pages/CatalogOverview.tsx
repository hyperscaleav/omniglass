import { For, Show, createMemo } from "solid-js";
import { A } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import { navItems, filterNav, type NavChild } from "../lib/nav";
import { useMe, can } from "../lib/auth";
import { PRODUCTS_KEY, listProducts } from "../lib/products";
import { VENDORS_KEY, listVendors } from "../lib/vendors";
import { DRIVERS_KEY, listDrivers } from "../lib/drivers";
import { CAPABILITIES_KEY, listCapabilities } from "../lib/capabilities";
import { STANDARDS_KEY, listStandards } from "../lib/standards";
import { LOCATION_TYPES_KEY, listLocationTypes } from "../lib/location_types";
import { SECRET_TYPES_KEY, listSecretTypes } from "../lib/secret_types";
import { METRICS_KEY, listMetricTypes } from "../lib/metric_types";
import { PROPERTIES_KEY, listProperties } from "../lib/properties";
import { EVENT_TYPES_KEY, listEventTypes } from "../lib/event_types";
import { COMMAND_TYPES_KEY, listCommandTypes } from "../lib/command_types";
import { TAGS_KEY, listTags } from "../lib/tags";

// The Catalog overview (/catalog, #609): the hub the rail's Overview entry
// opens. One card per Catalog section, DERIVED from the same navItems the
// sidebar renders and filtered through the same filterNav gate, so membership
// and visibility can never drift from the rail: a section whose entries are all
// gated away renders no card (the floor viewer's missing Secrets, #598). Each
// card teaches its section and counts its registries live through the list
// pages' own query keys (learning-tool doctrine: the numbers are this estate's
// real rows, not decoration); an unlive entry reads soon, without a count. One
// query per registry, so a failing fetch marks its own row and nothing else.

type Registry = { key: readonly string[]; list: () => Promise<readonly unknown[]> };

// Each live entry's count source, keyed by the entry's nav path and reusing the
// list page's cache key and fn, so the count here and the list there are one
// cache entry: navigating from a card refetches nothing.
export const REGISTRIES: Record<string, Registry> = {
  "/products": { key: PRODUCTS_KEY, list: listProducts },
  "/vendors": { key: VENDORS_KEY, list: listVendors },
  "/drivers": { key: DRIVERS_KEY, list: listDrivers },
  "/capabilities": { key: CAPABILITIES_KEY, list: listCapabilities },
  "/standards": { key: STANDARDS_KEY, list: listStandards },
  "/location-types": { key: LOCATION_TYPES_KEY, list: listLocationTypes },
  "/secret-types": { key: SECRET_TYPES_KEY, list: listSecretTypes },
  "/metrics": { key: METRICS_KEY, list: listMetricTypes },
  "/properties": { key: PROPERTIES_KEY, list: listProperties },
  "/event-types": { key: EVENT_TYPES_KEY, list: listEventTypes },
  "/command-types": { key: COMMAND_TYPES_KEY, list: listCommandTypes },
  "/tags": { key: TAGS_KEY, list: listTags },
};

// The teaching copy, one narrative sentence or two per section (the section
// list itself derives from the nav; only this prose is authored here).
export const SECTION_COPY: Record<string, string> = {
  Components:
    "A component points at a product (its shape), a product names its vendor, a driver is how we talk to it, and capabilities say what it can do.",
  Systems: "A standard is the blueprint a system conforms to: the properties every conforming system exposes.",
  Locations: "A location type classifies a place (campus, building, floor, room) and carries its contract.",
  Secrets: "A secret type is the shape a secret takes; seed-owned, read-only reference data.",
  Telemetry:
    "What arrives, typed by lane: metrics (quantities), properties (values), events (happenings). Logs arrive untyped today; their catalog is future.",
  Action:
    "What the platform does: rules (what raises alarms), commands (the instructions you issue, with target and settle window), notifications (soon, the outbound one). Doings get remembered as caused events in Telemetry.",
  General: "Tags: the governed key vocabulary the whole estate labels with.",
};

type Section = { label: string; entries: NavChild[] };

export default function CatalogOverview() {
  const me = useMe();
  // The card model: the Catalog children the caller may see, grouped into their
  // runs of shared section, in nav order. The unsectioned Overview entry (this
  // page's own rail address) carries no section and drops out here.
  const sections = createMemo<Section[]>(() => {
    const cat = filterNav(navItems, (tokens) => can(me.data, ...tokens)).find((i) => i.label === "Catalog");
    const out: Section[] = [];
    for (const c of cat?.children ?? []) {
      if (!c.section) continue;
      const last = out[out.length - 1];
      if (last && last.label === c.section) last.entries.push(c);
      else out.push({ label: c.section, entries: [c] });
    }
    return out;
  });

  return (
    <Page
      title="Catalog"
      subtitle="The authored model behind the estate: what each section's registries hold, counted live from this install."
    >
      <div class="grid grid-cols-[repeat(auto-fill,minmax(280px,1fr))] gap-4">
        <For each={sections()}>{(s) => <SectionCard section={s} />}</For>
      </div>
    </Page>
  );
}

function SectionCard(props: { section: Section }) {
  return (
    <section class="card border border-base-300 bg-base-200" data-card={props.section.label}>
      <div class="card-body gap-2 p-5">
        <h2 class="card-title text-base">{props.section.label}</h2>
        <p class="text-sm text-base-content/60">{SECTION_COPY[props.section.label]}</p>
        <ul class="mt-1 flex flex-col gap-0.5">
          <For each={props.section.entries}>
            {(e) => (
              <li>
                <EntryRow entry={e} registryName={`${props.section.label} ${e.label}`} />
              </li>
            )}
          </For>
        </ul>
      </div>
    </section>
  );
}

function EntryRow(props: { entry: NavChild; registryName: string }) {
  // The registry is keyed by the entry's static nav path; a row never swaps its
  // entry (For keys by reference), so this lookup is not a tracked read.
  const reg = REGISTRIES[props.entry.path];
  return (
    <A
      href={props.entry.path}
      class="-mx-2 flex items-center justify-between gap-2 rounded-field px-2 py-1.5 hover:bg-base-300"
      title={props.entry.hint}
    >
      <span class="truncate text-sm" classList={{ "text-base-content/50": !props.entry.live }}>
        {props.entry.label}
      </span>
      <Show when={props.entry.live && reg} fallback={<span class="badge badge-ghost badge-sm text-base-content/40">soon</span>}>
        <Count reg={reg} name={props.registryName} />
      </Show>
    </A>
  );
}

// Count is one registry's live number: its own query on the list page's shared
// key, with its own skeleton and its own failure. A refused or broken fetch
// marks only this row (the #598 lesson: no aggregate query whose one rejection
// takes every count down).
function Count(props: { reg: Registry; name: string }) {
  const q = useQuery(() => ({ queryKey: props.reg.key, queryFn: props.reg.list }));
  return (
    <Show
      when={!q.isError}
      fallback={
        <span class="badge badge-sm border-error/40 text-error" title={`The ${props.name} registry did not load. The rest of the page stands; retry from its own page.`}>
          unavailable
        </span>
      }
    >
      <Show when={q.data} fallback={<span class="skeleton h-4 w-7" aria-hidden="true" />}>
        {(rows) => <span class="tnum badge badge-ghost badge-sm">{rows().length}</span>}
      </Show>
    </Show>
  );
}
