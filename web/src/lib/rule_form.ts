import type { PickerOption } from "../components/ui/Picker";
import type { Component } from "./components";
import { entityLabel } from "./entities";
import type { MetricRow } from "./metric_types";
import type { PropertyRow } from "./properties";
import { firstParam } from "./drawer_address";
import { DRAWER_PAGES } from "./drawer_address";

// The pure half of the rule form's pickers and its URL prefill (#2432): the
// options each filterable picker draws, and the params "Alarm on this" opens the
// form with. No I/O, so each is unit-tested without a component.

const byLabel = (a: { label?: string; name: string }, b: { label?: string; name: string }) => entityLabel(a).localeCompare(entityLabel(b));

// "<lane>:<key>" is how the Reads picker holds its value: the lane decides the
// comparisons and the value's type, and it is the catalog the key sits in.
export const readsValue = (lane: "metric" | "property", key: string) => `${lane}:${key}`;

export type ReadsSources = {
  metrics: Pick<MetricRow, "name" | "label" | "unit">[];
  properties: Pick<PropertyRow, "name" | "label">[];
  // The component the rule watches, when it watches exactly one: the keys it
  // effectively has (as "<lane>:<key>") list first under its own heading.
  onComponent?: { label: string; keys: string[] };
};

// readsOptions: every metric and property, grouped by kind, each showing its
// label with the name and unit beside it. A watched component's own keys come
// first under "On <component>" (and are not repeated below), the kind then
// riding in the detail since the heading no longer says it.
export function readsOptions(src: ReadsSources): PickerOption[] {
  const own = new Set(src.onComponent?.keys ?? []);
  const all: { lane: "metric" | "property"; label: string; detail: string; value: string }[] = [
    ...[...src.metrics].sort(byLabel).map((m) => ({
      lane: "metric" as const, value: readsValue("metric", m.name), label: entityLabel(m), detail: m.unit ? `${m.name} · ${m.unit}` : m.name,
    })),
    ...[...src.properties].sort(byLabel).map((p) => ({
      lane: "property" as const, value: readsValue("property", p.name), label: entityLabel(p), detail: p.name,
    })),
  ];
  const head = src.onComponent
    ? all
        .filter((o) => own.has(o.value))
        .map((o) => ({ value: o.value, label: o.label, detail: `${o.lane} · ${o.detail}`, group: `On ${src.onComponent!.label}` }))
    : [];
  const rest = all
    .filter((o) => !own.has(o.value) || !src.onComponent)
    .map((o) => ({ value: o.value, label: o.label, detail: o.detail, group: o.lane === "metric" ? "Metrics" : "Properties" }));
  return [...head, ...rest.filter((o) => o.group === "Metrics"), ...rest.filter((o) => o.group === "Properties")];
}

// placeOf is where a component sits, as one line: the system it serves and the
// location it stands in. Labels repeat across a fleet ("Display 1" forty times);
// the place is what tells them apart.
export function placeOf(c: Pick<Component, "location" | "system">): string {
  return [c.location, c.system].filter(Boolean).join(" · ") || "unplaced";
}

export function componentOptions(components: Pick<Component, "id" | "name" | "label" | "location" | "system">[]): PickerOption[] {
  return [...components].sort(byLabel).map((c) => ({ value: c.id, label: entityLabel(c), detail: placeOf(c) }));
}

// humanizeName reads a kebab name as words for a label field's suggestion:
// hyphens become spaces and the first letter is capitalised, nothing else, so an
// acronym the operator wrote (DSP) is left as typed and no word is re-cased.
export function humanizeName(name: string): string {
  const s = name.trim().replace(/-+/g, " ").trim();
  return s ? s[0].toUpperCase() + s.slice(1) : "";
}

// ---- prefill: /event-rules?new=1&reads=<key>&component=<id> -----------------

// readsKeyFromParam resolves the URL's bare key to the picker's "<lane>:<key>"
// through the catalogs; undefined when neither holds it.
export function readsKeyFromParam(key: string, src: Pick<ReadsSources, "metrics" | "properties">): string | undefined {
  if (src.metrics.some((m) => m.name === key)) return readsValue("metric", key);
  if (src.properties.some((p) => p.name === key)) return readsValue("property", key);
  return undefined;
}

export type RuleFormPrefill = { reads?: string; component?: string };

export function ruleFormPrefill(params: Record<string, string | string[] | undefined>): RuleFormPrefill {
  const out: RuleFormPrefill = {};
  const reads = firstParam(params.reads);
  const component = firstParam(params.component);
  if (reads) out.reads = reads;
  if (component) out.component = component;
  return out;
}

// alarmOnThisHref opens the new-rule form reading `reads` on one component.
export function alarmOnThisHref(reads: string, componentId: string): string {
  const q = new URLSearchParams({ new: "1", reads, component: componentId });
  return `${DRAWER_PAGES.event_rule}?${q.toString()}`;
}
