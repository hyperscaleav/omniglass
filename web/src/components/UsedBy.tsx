import { For, Show, createSignal, type JSX } from "solid-js";
import { A, RouterContext } from "@solidjs/router";
import { useContext } from "solid-js";
import { USED_BY_PAGE } from "../lib/fleet_usage";

// UsedBy is the one "what uses this" section a catalog drawer carries (#2430): a
// titled list of groups, each a label, a count and the first few rows as links.
// Twelve drawers render it; none hand-rolls one. A group's rows are what the page
// could name (a catalog row's link, a component's page); its count is the whole
// truth, which may be longer than the rows when the server capped the page. A
// count longer than the rows offers `seeAll`, the filtered list; a group with
// every row in hand and more than a page of them folds the rest behind "Show all".
export type UsedByRow = { key: string; element: JSX.Element };
export type UsedByGroup = {
  label: string;
  count: number;
  rows: UsedByRow[];
  // Where the whole filtered list lives, for a group whose rows are a capped page.
  seeAll?: { href: string; text?: string };
  // The group's read has not answered yet.
  loading?: boolean;
  // What an empty group says; defaults to "None".
  empty?: string;
};

const slug = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/(^-|-$)/g, "");

export default function UsedBy(props: { groups: UsedByGroup[]; limit?: number }): JSX.Element {
  const limit = () => props.limit ?? USED_BY_PAGE;
  return (
    <Show when={props.groups.length > 0}>
      <section class="flex flex-col gap-3 border-t border-base-300 pt-3" aria-label="Used by" data-testid="used-by">
        <h3 class="eyebrow">Used by</h3>
        <For each={props.groups}>{(g) => <Group group={g} limit={limit()} />}</For>
      </section>
    </Show>
  );
}

function Group(props: { group: UsedByGroup; limit: number }): JSX.Element {
  const [all, setAll] = createSignal(false);
  const inRouter = !!useContext(RouterContext);
  const shown = () => (all() ? props.group.rows : props.group.rows.slice(0, props.limit));
  const hidden = () => props.group.rows.length - shown().length;
  return (
    <div data-testid={`used-by-${slug(props.group.label)}`} class="flex flex-col gap-1">
      <div class="flex items-baseline gap-2 text-sm">
        <span class="font-medium">{props.group.label}</span>
        <span class="badge badge-ghost badge-sm tabular-nums" data-testid="used-by-count">
          {props.group.loading ? "…" : props.group.count}
        </span>
      </div>
      <Show
        when={!props.group.loading && props.group.rows.length > 0}
        fallback={
          <Show when={!props.group.loading}>
            <p class="text-xs text-base-content/45">{props.group.empty ?? "None"}</p>
          </Show>
        }
      >
        <ul class="flex flex-col gap-0.5 text-sm">
          <For each={shown()}>{(r) => <li class="flex min-w-0">{r.element}</li>}</For>
        </ul>
        <div class="flex gap-3 text-xs">
          <Show when={hidden() > 0}>
            <button type="button" class="text-primary hover:underline" onClick={() => setAll(true)}>
              Show all {props.group.rows.length}
            </button>
          </Show>
          <Show when={props.group.seeAll && props.group.count > props.group.rows.length}>
            <Show
              when={inRouter}
              fallback={<a class="text-primary hover:underline" href={props.group.seeAll!.href}>{props.group.seeAll!.text ?? `See all ${props.group.count}`}</a>}
            >
              <A class="text-primary hover:underline" href={props.group.seeAll!.href}>{props.group.seeAll!.text ?? `See all ${props.group.count}`}</A>
            </Show>
          </Show>
        </div>
      </Show>
    </div>
  );
}
