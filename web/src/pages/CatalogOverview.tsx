import { For, Show, createMemo, type JSX } from "solid-js";
import { A } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import { useMe, can } from "../lib/auth";
import { visibleGroups, type CatalogEntry, type CatalogGroup } from "../lib/catalog";

// The Catalog overview (/catalog): the landing the shell's Overview entry
// opens, inside the shell's pane. One card per catalog group, DERIVED from the
// SAME CATALOG_GROUPS table the subrail renders (lib/catalog.ts, one source of
// truth), so membership, order, teaching copy, and gating can never drift
// between the two surfaces: a group whose entries are all gated away renders
// no card, through the same visibleGroups over the same can() the rail uses.
// Each card teaches its group (the sentence lives on the table) and counts its
// registries live through the list pages' own query keys (learning-tool
// doctrine: the numbers are this estate's real rows, not decoration); a soon
// entry reads soon without a count, linking when its stub page is routed
// (Rules, Notifications) and inert when none exists (the Templates slots). One
// query per registry, so a failing fetch marks its own row and nothing else
// (the #598 lesson).

export default function CatalogOverview() {
  const me = useMe();
  const groups = createMemo(() => visibleGroups((tokens) => can(me.data, ...tokens)));

  return (
    <Page
      title="Catalog"
      subtitle="The authored model behind the estate: what each group's registries hold, counted live from this install."
    >
      <div class="grid grid-cols-[repeat(auto-fill,minmax(280px,1fr))] gap-4">
        <For each={groups()}>{(g) => <GroupCard group={g} />}</For>
      </div>
    </Page>
  );
}

function GroupCard(props: { group: CatalogGroup }) {
  return (
    <section class="card border border-base-300 bg-base-200" data-card={props.group.header}>
      <div class="card-body gap-2 p-5">
        <h2 class="card-title text-base">{props.group.header}</h2>
        <p class="text-sm text-base-content/60">{props.group.copy}</p>
        <ul class="mt-1 flex flex-col gap-0.5">
          <For each={props.group.entries}>
            {(e) => (
              <li>
                <EntryRow entry={e} registryName={`${props.group.header} ${e.label}`} />
              </li>
            )}
          </For>
        </ul>
      </div>
    </section>
  );
}

// One registry row: the label, then the live count (or the soon badge). An
// entry with a route is a link, a pageless soon slot is inert. The inner
// content is built per branch (a JSX element is a real DOM node; one node
// cannot stand in two places).
function EntryRow(props: { entry: CatalogEntry; registryName: string }) {
  const rowClass = "-mx-2 flex items-center justify-between gap-2 rounded-field px-2 py-1.5";
  const inner = (): JSX.Element => (
    <>
      <span class="truncate text-sm" classList={{ "text-base-content/50": props.entry.soon }}>
        {props.entry.label}
      </span>
      <Show
        when={!props.entry.soon}
        fallback={<span class="badge badge-ghost badge-sm text-base-content/40">soon</span>}
      >
        <Count entry={props.entry} name={props.registryName} />
      </Show>
    </>
  );
  return (
    <Show
      when={props.entry.path}
      fallback={
        <div class={rowClass} data-entry={props.entry.label}>
          {inner()}
        </div>
      }
    >
      <A href={props.entry.path!} class={`${rowClass} hover:bg-base-300`} data-entry={props.entry.label}>
        {inner()}
      </A>
    </Show>
  );
}

// Count is one registry's live number: its own query on the list page's shared
// key, with its own skeleton and its own failure. A refused or broken fetch
// marks only this row (the #598 lesson: no aggregate query whose one rejection
// takes every count down).
function Count(props: { entry: CatalogEntry; name: string }) {
  const q = useQuery(() => ({ queryKey: [...props.entry.key!], queryFn: props.entry.list! }));
  return (
    <Show
      when={!q.isError}
      fallback={
        <span
          class="badge badge-sm border-error/40 text-error"
          title={`The ${props.name} registry did not load. The rest of the page stands; retry from its own page.`}
        >
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
