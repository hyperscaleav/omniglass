import { type Accessor, type JSX, Show, createMemo, createSignal } from "solid-js";
import Button from "./Button";
import FilterBar from "./FilterBar";
import { buildPredicate, type Chip, type FilterKey } from "../lib/predicate";
import { describeError } from "../lib/format";

// ListShell is the chrome every list surface shares, body-agnostic: the FilterBar
// (it owns the chip state and applies the client-side predicate), the outer card,
// and the error banner. It hands the body its filtered rows and the chip state,
// and takes a `trailing` slot for the body's action rail (create, view controls).
// The body (FlatList or TreeList) renders the table and owns its own detail
// idiom, so the tree/flat difference never leaks in here.
//
// `rows` feeds both the FilterBar's value autocomplete and the predicate; the body
// receives the filtered subset. Filtering is client-side over what is loaded, the
// same contract the inventory lists and the audit trail already use.
export default function ListShell<T>(props: {
  filterKeys: FilterKey<T>[];
  rows: T[];
  placeholder?: string;
  initialChips?: Chip[];
  error?: unknown;
  errorLabel?: string;
  trailing?: JSX.Element;
  // Controlled chip state (optional): a body that filters tree-aware (TreeList)
  // owns its own chips and passes them in, so the shell drives the same FilterBar
  // without owning the predicate. Omit both to let the shell own the chips
  // (uncontrolled), which is the flat case (FlatList) and the plain-catalog case.
  chips?: Accessor<Chip[]>;
  onChips?: (chips: Chip[]) => void;
  // Fill mode (optional): the shell stretches to its parent's fixed height and
  // hands the surplus to the body, whose scroll region scrolls internally
  // instead of growing the page. The FilterBar header (and any footer) stay
  // pinned. Off (the default), the shell grows with content.
  fill?: boolean;
  // The `?open=` address the list could not resolve (lib/drawer_address): shows a
  // non-disclosing not-found notice above the list. The same words for a row that
  // does not exist and one the caller may not see; `onDismiss` clears the param.
  // Show "n of m" in the filter bar while chips filter. Opt-in: a body that
  // filters for itself (the tree) cannot report the shell's filtered count.
  counted?: boolean;
  missing?: { noun: string; address: string; onDismiss: () => void };
  children: (filtered: Accessor<T[]>, chips: Accessor<Chip[]>) => JSX.Element;
}) {
  const [ownChips, setOwnChips] = createSignal<Chip[]>(props.initialChips ?? []);
  const chips = () => (props.chips ? props.chips() : ownChips());
  const setChips = (c: Chip[]) => (props.onChips ? props.onChips(c) : setOwnChips(c));
  // Lazy: a controlled body that filters itself never reads `filtered`, so this
  // memo never runs for it (Solid memos are pull-based).
  const filtered = createMemo(() => props.rows.filter(buildPredicate(props.filterKeys, chips())));
  const count = () => (props.counted && chips().length > 0 ? { shown: filtered().length, total: props.rows.length } : undefined);
  return (
    <div class="og-stack flex flex-col" classList={{ "h-full min-h-0": props.fill }}>
      <Show when={props.error}>
        <div role="alert" class="alert alert-error alert-soft text-sm">
          <span>{props.errorLabel ? `${props.errorLabel}: ` : ""}{describeError(props.error)}</span>
        </div>
      </Show>
      <Show when={props.missing}>
        {(m) => (
          <div role="status" data-testid="drawer-miss" class="alert alert-warning alert-soft flex items-center justify-between text-sm">
            <span>No {m().noun} at “{m().address}”, or you cannot see it. The list is showing instead.</span>
            <Button size="xs" onClick={m().onDismiss}>Dismiss</Button>
          </div>
        )}
      </Show>
      <div class="card overflow-hidden border border-base-300 bg-base-200 p-0" classList={{ "flex min-h-0 flex-1 flex-col": props.fill }}>
        <div class="border-b border-base-300 px-3 py-2.5">
          <FilterBar
            keys={props.filterKeys}
            rows={props.rows}
            chips={chips()}
            onChips={setChips}
            bare
            clearable
            trailing={props.trailing}
            count={count()}
            placeholder={props.placeholder}
          />
        </div>
        {props.children(filtered, chips)}
      </div>
    </div>
  );
}
