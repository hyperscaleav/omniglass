import { Show, type JSX } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import EntityLink from "./EntityLink";
import type { DrawerKind } from "../lib/drawer_address";
import type { Labelled } from "../lib/entities";
import { VENDORS_KEY, listVendors } from "../lib/vendors";
import { DRIVERS_KEY, listDrivers } from "../lib/drivers";
import { PRODUCTS_KEY, listProducts } from "../lib/products";
import { COMPONENT_TYPES_KEY, listComponentTypes } from "../lib/component_types";
import { STANDARDS_KEY, listStandards } from "../lib/standards";
import { SYSTEM_TYPES_KEY, listSystemTypes } from "../lib/system_types";
import { LOCATION_TYPES_KEY, listLocationTypes } from "../lib/location_types";
import { severityLevelsKey, listSeverityLevels } from "../lib/severity";
import { NOTIFICATION_TYPES_KEY, listNotificationTypes } from "../lib/notification_types";
import { EVENT_RULES_KEY, listEventRules } from "../lib/event_rules";
import { CALC_RULES_KEY, listCalcRules } from "../lib/calc_rules";
import { ACTION_RULES_KEY, listActionRules } from "../lib/action_rules";
import { METRICS_KEY, listMetricTypes } from "../lib/metric_types";
import { PROPERTIES_KEY, listProperties } from "../lib/properties";
import { EVENT_TYPES_KEY, listEventTypes } from "../lib/event_types";
import { COMMAND_TYPES_KEY, listCommandTypes } from "../lib/command_types";
import { NOTIFICATION_TARGETS_KEY, listNotificationTargets } from "../lib/notification_targets";

// CatalogRef is a catalog drawer's reference to another catalog row (#2430): the
// stored handle, as a link that opens the row, reading as the row's label when the
// catalog is in hand and as the handle until it is. It shares the page pickers'
// query keys, so the catalog is usually already cached. Nothing here is a hand-built
// href: it is EntityLink, which is drawerHref.
//
// A kind the registry does not list (a user, a role, a secret: lists an operator may
// not read) links by its handle alone.
type Catalog = { key: readonly unknown[]; list: () => Promise<Labelled[]> };

const CATALOGS: Partial<Record<DrawerKind, Catalog>> = {
  vendor: { key: VENDORS_KEY, list: listVendors },
  driver: { key: DRIVERS_KEY, list: listDrivers },
  product: { key: PRODUCTS_KEY, list: listProducts },
  component_type: { key: COMPONENT_TYPES_KEY, list: listComponentTypes },
  standard: { key: STANDARDS_KEY, list: listStandards },
  system_type: { key: SYSTEM_TYPES_KEY, list: listSystemTypes },
  location_type: { key: LOCATION_TYPES_KEY, list: listLocationTypes },
  severity_level: { key: severityLevelsKey, list: listSeverityLevels },
  notification_type: { key: NOTIFICATION_TYPES_KEY, list: listNotificationTypes },
  event_rule: { key: EVENT_RULES_KEY, list: listEventRules },
  calc_rule: { key: CALC_RULES_KEY, list: listCalcRules },
  action_rule: { key: ACTION_RULES_KEY, list: listActionRules },
  metric: { key: METRICS_KEY, list: listMetricTypes },
  property: { key: PROPERTIES_KEY, list: listProperties },
  command_type: { key: COMMAND_TYPES_KEY, list: listCommandTypes },
  event_type: { key: EVENT_TYPES_KEY, list: listEventTypes },
  notification_target: { key: NOTIFICATION_TARGETS_KEY, list: listNotificationTargets },
};

export default function CatalogRef(props: { kind: DrawerKind; name: string | undefined | null }): JSX.Element {
  const catalog = CATALOGS[props.kind];
  const rows = useQuery(() => ({ queryKey: catalog?.key ?? ["catalog-ref", props.kind], queryFn: catalog?.list ?? (async () => []), enabled: !!catalog }));
  const row = () => (rows.data ?? []).find((r) => r.name === props.name);
  return (
    <Show when={props.name}>
      <Show when={row()} fallback={<EntityLink kind={props.kind} address={props.name!} />}>
        {(r) => <EntityLink kind={props.kind} entity={r()} />}
      </Show>
    </Show>
  );
}

// KeyRef links a sample key whose lane the caller does not know (a calc rule's
// input names a key, not a lane): a metric when the metric catalog has it, else a
// property when that has it. A key neither catalog holds yet (the catalogs still
// loading, or a key since deleted) reads as plain text rather than a link to nothing.
export function KeyRef(props: { name: string }): JSX.Element {
  const metrics = useQuery(() => ({ queryKey: METRICS_KEY, queryFn: listMetricTypes }));
  const properties = useQuery(() => ({ queryKey: PROPERTIES_KEY, queryFn: listProperties }));
  const kind = (): DrawerKind | undefined =>
    (metrics.data ?? []).some((m) => m.name === props.name) ? "metric" : (properties.data ?? []).some((x) => x.name === props.name) ? "property" : undefined;
  return (
    <Show when={kind()} fallback={<span class="font-data">{props.name}</span>}>
      {(k) => <CatalogRef kind={k()} name={props.name} />}
    </Show>
  );
}
