import { type Accessor, type JSX, Show, createMemo, createSignal } from "solid-js";
import FilterBar from "./FilterBar";
import { buildPredicate, type Chip, type FilterKey } from "../lib/predicate";
import { describeError } from "../lib/format";

// ListShell is the chrome every list surface shares, body-agnostic: the FilterBar
// (it owns the chip state and applies the client-side predicate), the outer card,
// and the error banner. It hands the body its filtered rows and the chip state,
// and takes a `trailing` slot for the body's action rail (create, view controls).
// The body (FlatList, and TreeList once the tree pages migrate) renders the table
// and owns its own detail idiom, so the tree/flat difference never leaks in here.
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
  children: (filtered: Accessor<T[]>, chips: Accessor<Chip[]>) => JSX.Element;
}) {
  const [ownChips, setOwnChips] = createSignal<Chip[]>(props.initialChips ?? []);
  const chips = () => (props.chips ? props.chips() : ownChips());
  const setChips = (c: Chip[]) => (props.onChips ? props.onChips(c) : setOwnChips(c));
  // Lazy: a controlled body that filters itself never reads `filtered`, so this
  // memo never runs for it (Solid memos are pull-based).
  const filtered = createMemo(() => props.rows.filter(buildPredicate(props.filterKeys, chips())));
  return (
    <div class="og-stack flex flex-col">
      <Show when={props.error}>
        <div role="alert" class="alert alert-error alert-soft text-sm">
          <span>{props.errorLabel ? `${props.errorLabel}: ` : ""}{describeError(props.error)}</span>
        </div>
      </Show>
      <div class="card overflow-hidden border border-base-300 bg-base-200 p-0">
        <div class="border-b border-base-300 px-3 py-2.5">
          <FilterBar
            keys={props.filterKeys}
            rows={props.rows}
            chips={chips()}
            onChips={setChips}
            bare
            clearable
            trailing={props.trailing}
            placeholder={props.placeholder}
          />
        </div>
        {props.children(filtered, chips)}
      </div>
    </div>
  );
}
