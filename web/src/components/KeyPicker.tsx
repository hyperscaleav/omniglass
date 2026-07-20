import { Show, createMemo, type JSX } from "solid-js";
import { Combobox } from "@kobalte/core/combobox";
import { useQuery } from "@tanstack/solid-query";
import { KEYS_KEY, listKeys, type KeyRow } from "../lib/keys";
import { Check, ChevronsUpDown } from "./icons";

// KeyPicker is the reusable catalog picker over the canonical keyspace (GET /keys):
// a searchable Kobalte Combobox whose options are registered keys, each shown with
// its data_type and label so the operator picks a real, typed key rather than
// minting a free string. It is deliberately generic (no field wiring): a caller
// passes the selected key name, an onSelect that receives the whole KeyRow (so it
// can surface the key's data_type and label), and optional filters. The field
// editor is its first consumer; the driver epic reuses it.
//
// The dropdown is PORTALED (Combobox.Portal) so it escapes the overflow of a blade
// or card, per the Kobalte portal rule. The trigger is a real button and lives
// outside any <label>, so hover/focus and label association behave.
export default function KeyPicker(props: {
  // The selected key name (controlled). Empty/undefined means nothing selected.
  value?: string;
  // Called with the picked key (its data_type and label ride along), or null when
  // the selection is cleared.
  onSelect: (key: KeyRow | null) => void;
  // Optional predicate to narrow the catalog (e.g. by kind or data_type). A key
  // that fails it never appears.
  filter?: (k: KeyRow) => boolean;
  // Key names to omit (e.g. already used on this type), so the picker never offers
  // a duplicate.
  exclude?: string[];
  disabled?: boolean;
  placeholder?: string;
  "aria-label"?: string;
}): JSX.Element {
  const keys = useQuery(() => ({ queryKey: KEYS_KEY, queryFn: listKeys }));

  // The offered options: the catalog, narrowed by the caller's filter and exclude
  // set, sorted by name for a stable order.
  const options = createMemo<KeyRow[]>(() => {
    const omit = new Set(props.exclude ?? []);
    return (keys.data ?? [])
      .filter((k) => !omit.has(k.name) && (props.filter ? props.filter(k) : true))
      .slice()
      .sort((a, b) => a.name.localeCompare(b.name));
  });

  // The selected option object, resolved from the controlled name.
  const selected = createMemo<KeyRow | null>(() => options().find((k) => k.name === props.value) ?? null);

  return (
    <Combobox<KeyRow>
      options={options()}
      optionValue="name"
      optionTextValue="name"
      optionLabel="name"
      value={selected()}
      onChange={(k) => props.onSelect(k)}
      disabled={props.disabled}
      placeholder={props.placeholder ?? "Search keys…"}
      // Also match the label so a search on the human name finds the key.
      defaultFilter={(option, inputValue) => {
        const q = inputValue.toLowerCase();
        return option.name.toLowerCase().includes(q) || (option.display_name ?? "").toLowerCase().includes(q);
      }}
      itemComponent={(iprops) => (
        <Combobox.Item
          item={iprops.item}
          class="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm outline-none data-[highlighted]:bg-base-200"
        >
          <Combobox.ItemLabel class="flex min-w-0 flex-1 items-center gap-2">
            <span class="truncate font-data">{iprops.item.rawValue.name}</span>
            <span class="badge badge-ghost badge-sm font-data">{iprops.item.rawValue.data_type}</span>
            <Show when={iprops.item.rawValue.display_name}>
              <span class="truncate text-xs text-base-content/50">{iprops.item.rawValue.display_name}</span>
            </Show>
          </Combobox.ItemLabel>
          <Combobox.ItemIndicator class="shrink-0 text-primary">
            <Check size={14} />
          </Combobox.ItemIndicator>
        </Combobox.Item>
      )}
    >
      <Combobox.Control class="flex" aria-label={props["aria-label"] ?? "Key"}>
        <div class="join w-full">
          <Combobox.Input class="input input-bordered input-sm join-item w-full font-data" />
          <Combobox.Trigger class="btn btn-sm join-item border-base-300" aria-label="Show keys">
            <Combobox.Icon>
              <ChevronsUpDown size={14} />
            </Combobox.Icon>
          </Combobox.Trigger>
        </div>
      </Combobox.Control>
      <Combobox.Portal>
        <Combobox.Content class="z-50 max-h-64 overflow-y-auto rounded-box border border-base-300 bg-base-100 p-1 shadow-lg">
          <Combobox.Listbox class="flex flex-col gap-0.5" />
        </Combobox.Content>
      </Combobox.Portal>
    </Combobox>
  );
}
