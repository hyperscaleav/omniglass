import { For, Show, createMemo, type JSX } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import EntityLink, { FleetLink } from "./EntityLink";
import UsedBy, { type UsedByGroup, type UsedByRow } from "./UsedBy";
import { entityLabel, type Labelled } from "../lib/entities";
import { can, useMe } from "../lib/auth";
import type { DrawerKind } from "../lib/drawer_address";
import { PRODUCTS_KEY, listProducts, type Product } from "../lib/products";
import { DRIVERS_KEY, listDrivers } from "../lib/drivers";
import { METRICS_KEY, listMetricTypes } from "../lib/metric_types";
import { PROPERTIES_KEY, listProperties } from "../lib/properties";
import { EVENT_TYPES_KEY, listEventTypes } from "../lib/event_types";
import { EVENT_RULES_KEY, listEventRules } from "../lib/event_rules";
import { CALC_RULES_KEY, listCalcRules } from "../lib/calc_rules";
import { ACTION_RULES_KEY, listActionRules } from "../lib/action_rules";
import {
  componentUsage,
  componentUsageKey,
  declaringKey,
  productsDeclaring,
  systemUsage,
  systemUsageKey,
  type ComponentFilter,
} from "../lib/fleet_usage";
import { driverCollects, driversCollecting, driversOffering, exploreHref, rulesReading } from "../lib/catalog_usage";

// The "Used by" and "Collected by" sections of the catalog drawers (#2430), one
// small component per catalog entity so a page mounts its section with one JSX line.
// Each builds its groups from rows the console already holds (a catalog is small
// reference data) and from the fleet list routes' own filters for the counts that
// would otherwise mean fetching the fleet; the rendering is the shared UsedBy.

const link = (kind: DrawerKind, entity: Labelled & { id?: string }): UsedByRow => ({
  key: entity.name,
  element: <EntityLink kind={kind} entity={entity} />,
});

// What a fleet group reads: a component filter's count and first rows. Disabled
// (no group at all) for a caller who cannot read components.
function useComponentGroup(label: string, filter: () => ComponentFilter, seeAll: () => string | undefined): () => UsedByGroup | undefined {
  const me = useMe();
  const q = useQuery(() => ({
    queryKey: componentUsageKey(filter()),
    queryFn: () => componentUsage(filter()),
    enabled: can(me.data, "component", "read"),
  }));
  return () =>
    can(me.data, "component", "read")
      ? {
          label,
          count: q.data?.total ?? 0,
          loading: q.isPending,
          rows: (q.data?.rows ?? []).map((c) => ({ key: c.id, element: <FleetLink kind="component" row={c} /> })),
          seeAll: seeAll() ? { href: seeAll()! } : undefined,
        }
      : undefined;
}

const present = (gs: (UsedByGroup | undefined)[]): UsedByGroup[] => gs.filter((g): g is UsedByGroup => !!g);

const useProducts = () => useQuery(() => ({ queryKey: PRODUCTS_KEY, queryFn: listProducts }));
const useDrivers = () => useQuery(() => ({ queryKey: DRIVERS_KEY, queryFn: listDrivers }));

// The Explore link over a set of products' labels, for a fleet group's See all.
const productChips = (ps: Product[]): string | undefined => (ps.length ? exploreHref("product", ps.map((p) => entityLabel(p))) : undefined);

export function ProductUsedBy(p: { name: string }): JSX.Element {
  const products = useProducts();
  const self = () => (products.data ?? []).find((x) => x.name === p.name);
  const components = useComponentGroup("Components", () => ({ product: p.name }), () => productChips(self() ? [self()!] : []));
  return <UsedBy groups={present([components()])} />;
}

export function VendorUsedBy(p: { name: string }): JSX.Element {
  const products = useProducts();
  const mine = createMemo(() => (products.data ?? []).filter((x) => x.vendor === p.name));
  return <UsedBy groups={[{ label: "Products", count: mine().length, loading: products.isPending, rows: mine().map((x) => link("product", x)) }]} />;
}

export function DriverUsedBy(p: { name: string }): JSX.Element {
  const products = useProducts();
  const mine = createMemo(() => (products.data ?? []).filter((x) => x.driver === p.name));
  const components = useComponentGroup("Components", () => ({ driver: p.name }), () => productChips(mine()));
  return (
    <UsedBy
      groups={present([
        { label: "Products", count: mine().length, loading: products.isPending, rows: mine().map((x) => link("product", x)) },
        components(),
      ])}
    />
  );
}

export function ComponentTypeUsedBy(p: { name: string }): JSX.Element {
  const products = useProducts();
  const mine = createMemo(() => (products.data ?? []).filter((x) => x.component_type === p.name));
  const components = useComponentGroup("Components", () => ({ component_type: p.name }), () => productChips(mine()));
  return (
    <UsedBy
      groups={present([
        { label: "Products", count: mine().length, loading: products.isPending, rows: mine().map((x) => link("product", x)) },
        components(),
      ])}
    />
  );
}

export function StandardUsedBy(p: { name: string; label: string }): JSX.Element {
  const me = useMe();
  const q = useQuery(() => ({
    queryKey: systemUsageKey(p.name),
    queryFn: () => systemUsage(p.name),
    enabled: can(me.data, "system", "read"),
  }));
  const group = (): UsedByGroup | undefined =>
    can(me.data, "system", "read")
      ? {
          label: "Systems",
          count: q.data?.total ?? 0,
          loading: q.isPending,
          rows: (q.data?.rows ?? []).map((s) => ({ key: s.id, element: <FleetLink kind="system" row={s} /> })),
          seeAll: { href: exploreHref("standard", [p.label]) },
        }
      : undefined;
  return <UsedBy groups={present([group()])} />;
}

// A metric's or a property's users: the products that declare it (a contract read
// the API answers by filter), the drivers that collect it, and the rules that read it.
export function KeyUsedBy(p: { lane: "metric" | "property"; name: string }): JSX.Element {
  const me = useMe();
  const declaring = useQuery(() => ({ queryKey: declaringKey(p.lane, p.name), queryFn: () => productsDeclaring(p.lane, p.name) }));
  const drivers = useDrivers();
  const canRules = () => can(me.data, "rule", "read");
  const eventRules = useQuery(() => ({ queryKey: EVENT_RULES_KEY, queryFn: listEventRules, enabled: canRules() }));
  const calcRules = useQuery(() => ({ queryKey: CALC_RULES_KEY, queryFn: listCalcRules, enabled: canRules() }));
  const collectors = createMemo(() => driversCollecting(drivers.data ?? [], p.name));
  const readers = createMemo(() => rulesReading(eventRules.data ?? [], calcRules.data ?? [], p.name));
  const ruleRows = (): UsedByRow[] => [
    ...readers().event.map((r) => link("event_rule", r)),
    ...readers().calc.map((r) => link("calc_rule", r)),
  ];
  return (
    <UsedBy
      groups={present([
        { label: "Products that declare it", count: (declaring.data ?? []).length, loading: declaring.isPending, rows: (declaring.data ?? []).map((x) => link("product", x)) },
        { label: "Drivers that collect it", count: collectors().length, loading: drivers.isPending, rows: collectors().map((d) => link("driver", d)) },
        canRules()
          ? { label: "Rules that read it", count: ruleRows().length, loading: eventRules.isPending || calcRules.isPending, rows: ruleRows() }
          : undefined,
      ])}
    />
  );
}

export function CommandTypeUsedBy(p: { name: string }): JSX.Element {
  const drivers = useDrivers();
  const mine = createMemo(() => driversOffering(drivers.data ?? [], p.name));
  return <UsedBy groups={[{ label: "Drivers that offer it", count: mine().length, loading: drivers.isPending, rows: mine().map((d) => link("driver", d)) }]} />;
}

export function SeverityUsedBy(p: { name: string }): JSX.Element {
  const me = useMe();
  const eventRules = useQuery(() => ({ queryKey: EVENT_RULES_KEY, queryFn: listEventRules, enabled: can(me.data, "rule", "read") }));
  const mine = createMemo(() => (eventRules.data ?? []).filter((r) => r.severity === p.name));
  return (
    <Show when={can(me.data, "rule", "read")}>
      <UsedBy groups={[{ label: "Rules", count: mine().length, loading: eventRules.isPending, rows: mine().map((r) => link("event_rule", r)) }]} />
    </Show>
  );
}

export function NotificationTypeUsedBy(p: { name: string }): JSX.Element {
  const me = useMe();
  const actionRules = useQuery(() => ({ queryKey: ACTION_RULES_KEY, queryFn: listActionRules, enabled: can(me.data, "rule", "read") }));
  const mine = createMemo(() => (actionRules.data ?? []).filter((r) => r.notification_type === p.name));
  return (
    <Show when={can(me.data, "rule", "read")}>
      <UsedBy groups={[{ label: "Action rules", count: mine().length, loading: actionRules.isPending, rows: mine().map((r) => link("action_rule", r)) }]} />
    </Show>
  );
}

// CollectedByDriver is what a driver collects and offers, from its spec: the
// metrics and properties its polls and listeners emit, and the command types it
// binds. On a product it is "Collected by its driver", what a component of the
// product will actually report, distinct from the contract the product declares;
// on the driver it is "Collects". A key neither catalog names still shows, as plain
// text, because it is a fact the spec states and a link to nothing would be a lie.
export function CollectedByDriver(p: { driver: string; title: string }): JSX.Element {
  const drivers = useDrivers();
  const metrics = useQuery(() => ({ queryKey: METRICS_KEY, queryFn: listMetricTypes }));
  const properties = useQuery(() => ({ queryKey: PROPERTIES_KEY, queryFn: listProperties }));
  const events = useQuery(() => ({ queryKey: EVENT_TYPES_KEY, queryFn: listEventTypes }));
  const row = () => (drivers.data ?? []).find((d) => d.name === p.driver);
  const collected = createMemo(() =>
    driverCollects(row()?.spec, new Set((metrics.data ?? []).map((m) => m.name)), new Set((properties.data ?? []).map((x) => x.name)), new Set((events.data ?? []).map((e) => e.name))),
  );
  const none = () => {
    const c = collected();
    return c.metrics.length + c.properties.length + c.events.length + c.commands.length + c.unknown.length === 0;
  };
  return (
    <section class="flex flex-col gap-2 border-t border-base-300 pt-3" aria-label={p.title} data-testid="collected-by-driver">
      <h3 class="eyebrow">{p.title}</h3>
      <Show when={row()} fallback={<p class="text-xs text-base-content/45">{drivers.isPending ? "…" : "This driver is not in the catalog."}</p>}>
        <Show when={!none()} fallback={<p class="text-xs text-base-content/45">The driver collects nothing and offers no commands.</p>}>
          <KeyList label="Metrics" kind="metric" names={collected().metrics} />
          <KeyList label="Properties" kind="property" names={collected().properties} />
          <KeyList label="Events" kind="event_type" names={collected().events} />
          <KeyList label="Commands" kind="command_type" names={collected().commands} />
          <Show when={collected().unknown.length > 0}>
            <div data-testid="collected-unknown" class="flex flex-col gap-1">
              <span class="eyebrow">Not in the catalogs</span>
              <div class="flex flex-wrap gap-1">
                <For each={collected().unknown}>{(k) => <span class="badge badge-outline badge-sm font-data">{k}</span>}</For>
              </div>
            </div>
          </Show>
        </Show>
      </Show>
    </section>
  );
}

function KeyList(p: { label: string; kind: DrawerKind; names: string[] }): JSX.Element {
  return (
    <Show when={p.names.length > 0}>
      <div data-testid={`collected-${p.label.toLowerCase()}`} class="flex flex-col gap-1">
        <span class="eyebrow">{p.label} <span class="tabular-nums text-base-content/40">{p.names.length}</span></span>
        <ul class="flex flex-wrap gap-x-3 gap-y-0.5 text-sm">
          <For each={p.names}>{(n) => <li><EntityLink kind={p.kind} address={n} /></li>}</For>
        </ul>
      </div>
    </Show>
  );
}

