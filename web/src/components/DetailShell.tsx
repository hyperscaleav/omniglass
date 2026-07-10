import { type JSX, For, Show, createSignal } from "solid-js";
import { ChevronRight, Plus, Trash } from "./icons";
import Button from "./Button";

// DetailShell: the shared body chrome every entity detail wears, so a user, a
// group, a role, and a location all read the same. Two pieces:
//   Fact          a labelled value in the facts grid.
//   RelatedList   a list of related entities (members, groups, children): one row
//                 idiom, an optional drill (onOpen -> a blade), an optional remove,
//                 an optional add-picker. Group members and a user's groups both
//                 render through this, so "related principals" looks identical
//                 wherever it appears.
// The footer action bar (Edit / Delete / secondary) is not here: it is chrome the
// BladeStack owns, driven by what the body registers (see lib/blades).

export function Fact(props: { label: string; value: JSX.Element }): JSX.Element {
  return (
    <div>
      <div class="eyebrow mb-1.5">{props.label}</div>
      <div class="text-sm">{props.value}</div>
    </div>
  );
}

export type RelatedItem = { id: string; kind: string; name: string; sub?: string; badge?: string };

export function RelatedList(props: {
  label: string;
  items: RelatedItem[];
  empty: string;
  // Present -> the row is a button that drills (opens item.kind's blade).
  onOpen?: (item: RelatedItem) => void;
  // Present -> each row carries a remove (Trash) button.
  onRemove?: (item: RelatedItem) => void;
  // Present -> an add-a-member picker under the list.
  add?: { placeholder: string; options: { id: string; label: string }[]; onAdd: (id: string) => void; canAdd: boolean };
}): JSX.Element {
  const [toAdd, setToAdd] = createSignal("");
  const rowBase = "flex items-center justify-between gap-2 rounded-field border border-base-300 bg-base-100 px-2.5 py-1.5";

  const Body = (p: { item: RelatedItem }) => (
    <>
      <span class="min-w-0 truncate">
        <span class="font-data text-sm">{p.item.name}</span>
        <Show when={p.item.badge}><span class="ml-1.5 badge badge-ghost badge-xs">{p.item.badge}</span></Show>
        <Show when={p.item.sub}><span class="ml-1.5 text-xs text-base-content/40">{p.item.sub}</span></Show>
      </span>
      <span class="flex flex-none items-center gap-1">
        <Show when={props.onRemove}>
          <Button
            square
            size="xs"
            icon={Trash}
            title="Remove"
            label="Remove"
            class="text-base-content/50"
            onClick={(e) => { e.stopPropagation(); props.onRemove!(p.item); }}
          />
        </Show>
        <Show when={props.onOpen}><ChevronRight size={14} /></Show>
      </span>
    </>
  );

  return (
    <div class="flex flex-col gap-1.5">
      <div class="eyebrow">{props.label}</div>
      <div class="flex flex-col gap-1.5">
        <For each={props.items} fallback={<p class="text-sm text-base-content/40">{props.empty}</p>}>
          {(item) => (
            <Show
              when={props.onOpen}
              fallback={<div class={rowBase}><Body item={item} /></div>}
            >
              <button class={`${rowBase} cursor-pointer text-left hover:bg-base-content/5`} onClick={() => props.onOpen!(item)}>
                <Body item={item} />
              </button>
            </Show>
          )}
        </For>
      </div>
      <Show when={props.add?.canAdd}>
        <div class="mt-1 flex items-center gap-2">
          <select class="select select-bordered select-sm min-w-0 flex-1 font-data" value={toAdd()} onChange={(e) => setToAdd(e.currentTarget.value)}>
            <option value="">{props.add!.placeholder}</option>
            <For each={props.add!.options}>{(o) => <option value={o.id}>{o.label}</option>}</For>
          </select>
          <Button intent="action" icon={Plus} disabled={!toAdd()} onClick={() => { props.add!.onAdd(toAdd()); setToAdd(""); }}>Add</Button>
        </div>
      </Show>
    </div>
  );
}

