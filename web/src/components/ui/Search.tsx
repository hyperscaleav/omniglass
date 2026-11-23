import { type JSX, Show, createMemo } from "solid-js";
import { type SearchRootItemComponentProps, Content, Control, Input, Item, ItemDescription, ItemLabel, Listbox, NoResult, Root } from "@kobalte/core/search";
import { Search as SearchIcon } from "../icons";

// Search: a type-to-find field with grouped results the SERVER has already
// filtered (#2444), over Kobalte Search (a Combobox whose filtering is external).
// The command palette's input and list; any later "find anything" surface uses it
// too, instead of a hand-rolled role="combobox".
//
// The caller owns the query and the results: `onQuery` reports each keystroke
// (debounce and cancel there), `sections` are the groups to show, `loading` adds
// a status row while a request is out. Nothing is filtered here.
//
// The list is NOT portalled: it renders inside whatever dialog holds the field.
// A portalled list opened together with its dialog would sit outside it and be
// hidden from a screen reader by the dialog's own aria-hidden sweep.
//
// Keyboard: the arrows move a highlight while focus stays in the field, Enter
// picks the highlighted row, and with nothing highlighted Enter picks the FIRST
// row (drawn as the default until the arrows move), so typing a name and pressing
// Enter opens its best match. Esc calls `onEscape`: the list is always open, so
// there is nothing to close but the surface around it.
export interface SearchOption {
  // Unique across every section: it is the row's identity.
  value: string;
  label: string;
  // Secondary text that tells two same-named rows apart (where it sits).
  detail?: string;
  icon?: JSX.Element;
}

export interface SearchSection {
  label: string;
  options: SearchOption[];
}

export interface SearchProps {
  sections: SearchSection[];
  loading?: boolean;
  emptyText?: string;
  placeholder?: string;
  "aria-label": string;
  onQuery: (text: string) => void;
  onPick: (option: SearchOption) => void;
  onEscape: () => void;
  class?: string;
  listClass?: string;
}

// Kobalte keys a section by the same field as an option, so each carries one.
// A row as the list holds it: the option, its group's name, and whether it opens
// that group (the first row of each).
type Row = SearchOption & { group: string; groupStart: boolean };

export default function Search(props: SearchProps) {
  // Flat, not Kobalte sections: its collection keys every section "" so only one
  // section's heading survives a render. The group heading is drawn on the first
  // row of the group instead (a ::before from data-group-start), and the group's
  // name is read to a screen reader ahead of that row's text.
  const rows = createMemo<Row[]>(() =>
    props.sections.flatMap((s) => s.options.map((o, i) => ({ ...o, group: s.label, groupStart: i === 0 }))),
  );
  const firstValue = () => rows()[0]?.value;
  const flat = rows;

  // With no row highlighted, Enter takes the first. Kobalte's own Enter picks the
  // highlighted one, so this only handles the unhighlighted case.
  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key !== "Enter") return;
    const highlighted = document.querySelector("[role=option][data-highlighted]");
    if (highlighted) return;
    const first = flat()[0];
    if (!first) return;
    e.preventDefault();
    e.stopPropagation();
    props.onPick(first);
  };

  return (
    <Root<Row>
      open
      options={rows()}
      optionValue="value"
      optionTextValue="label"
      optionLabel="label"
      onInputChange={props.onQuery}
      onChange={(o) => o && props.onPick(o)}
      onOpenChange={(o) => !o && props.onEscape()}
      debounceOptionsMillisecond={0}
      sameWidth
      gutter={0}
      placeholder={props.placeholder}
      itemComponent={(p: SearchRootItemComponentProps<Row>) => (
        <Item
          item={p.item}
          data-first={p.item.rawValue.value === firstValue() ? "" : undefined}
          data-group-start={p.item.rawValue.groupStart ? p.item.rawValue.group : undefined}
          class="group block cursor-pointer outline-none before:block before:px-3 before:pb-1 before:pt-2 before:text-xs before:font-medium before:uppercase before:tracking-wide before:text-base-content/40 before:content-[attr(data-group-start)] not-data-[group-start]:before:hidden"
        >
          <div class="flex items-center gap-3 rounded-field px-3 py-2 text-sm group-data-[highlighted]:bg-primary/15 group-data-[highlighted]:text-primary">
            <Show when={p.item.rawValue.icon}>{p.item.rawValue.icon}</Show>
            <ItemLabel class="flex-1 truncate">
              <span class="sr-only">{p.item.rawValue.group}: </span>
              {p.item.rawValue.label}
            </ItemLabel>
            <Show when={p.item.rawValue.detail}>
              <ItemDescription class="truncate text-xs font-normal text-base-content/50">{p.item.rawValue.detail}</ItemDescription>
            </Show>
          </div>
        </Item>
      )}
    >
      <Control class={props.class ?? "flex items-center gap-2 border-b border-base-300 px-4"}>
        <SearchIcon size={16} />
        <Input
          aria-label={props["aria-label"]}
          data-field-target=""
          autofocus
          onKeyDown={onKeyDown}
          class="h-12 w-full bg-transparent text-sm outline-none placeholder:text-base-content/40"
        />
      </Control>
        <Content class="z-100 overflow-hidden rounded-box border border-base-300 bg-base-100 shadow-2xl">
          <Show when={props.loading}>
            <p role="status" class="px-3 py-2 text-xs text-base-content/50">
              Searching…
            </p>
          </Show>
          <Listbox
            class={
              (props.listClass ?? "max-h-[28rem] overflow-y-auto p-2 focus:outline-none") +
              " [&:not(:has([data-highlighted]))_[data-first]>div]:bg-primary/15 [&:not(:has([data-highlighted]))_[data-first]>div]:text-primary"
            }
          />
          <NoResult class="px-3 py-6 text-center text-sm text-base-content/40">{props.emptyText ?? "No matches"}</NoResult>
        </Content>
    </Root>
  );
}
