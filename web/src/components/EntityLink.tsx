import { Show, useContext, type JSX } from "solid-js";
import { A, RouterContext } from "@solidjs/router";
import { drawerHref, type DrawerKind } from "../lib/drawer_address";
import { entityLabel, type Labelled } from "../lib/entities";

// EntityLink is the console's one link to an entity another page owns (#2430): a
// reference a drawer renders (a product's vendor, a rule's severity) becomes a way
// to open the thing it names, at its address, through drawerHref. Never build the
// href by hand and never render a reference as bare text: the catalog drawers'
// link guard fails a page that does.
//
// Pass the catalog ROW when the page holds it (`entity`), and the link reads as the
// operator reads that row everywhere else, its label through entityLabel. Pass the
// bare `address` (the API handle a record stores) when the row is not in hand, and
// the link reads as that handle in the data face, which is honest about it being an
// identifier. `extra` carries further search params, as drawerHref's does.
//
// Outside a Router (a unit test, an embed) the link is a plain anchor with the same
// href: the address is still right, it just does not navigate in-app.
type EntityLinkProps = {
  kind: DrawerKind;
  extra?: Record<string, string>;
  class?: string;
} & ({ entity: Labelled; address?: never } | { address: string; entity?: never });

export default function EntityLink(props: EntityLinkProps): JSX.Element {
  const inRouter = !!useContext(RouterContext);
  const address = () => props.entity?.name ?? props.address!;
  const href = () => drawerHref(props.kind, address(), props.extra);
  const text = () => (props.entity ? entityLabel(props.entity) : address());
  const cls = () => `link link-hover truncate ${props.entity && text() !== address() ? "" : "font-data"} ${props.class ?? ""}`;
  return (
    <Show
      when={inRouter}
      fallback={<a href={href()} class={cls()} data-entity-link={props.kind}>{text()}</a>}
    >
      <A href={href()} class={cls()} data-entity-link={props.kind}>{text()}</A>
    </Show>
  );
}

// FleetLink is the same link for a fleet entity, which has a page of its own rather
// than a list drawer: a component's or a system's workspace, by its uuid (a name is
// unique only within its placement, so the id is the address that cannot be
// ambiguous). The text is the row's label as the console reads it everywhere.
export function FleetLink(props: { kind: "component" | "system"; row: Labelled & { id: string }; class?: string }): JSX.Element {
  const inRouter = !!useContext(RouterContext);
  const href = () => `/${props.kind === "component" ? "components" : "systems"}/${props.row.id}`;
  const cls = () => `link link-hover truncate ${props.class ?? ""}`;
  return (
    <Show when={inRouter} fallback={<a href={href()} class={cls()} data-fleet-link={props.kind}>{entityLabel(props.row)}</a>}>
      <A href={href()} class={cls()} data-fleet-link={props.kind}>{entityLabel(props.row)}</A>
    </Show>
  );
}
