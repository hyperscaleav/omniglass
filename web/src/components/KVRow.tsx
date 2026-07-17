import { Show, type JSX } from "solid-js";
import { ChevronRight } from "./icons";

// KVRow is the console's one key:value ROW primitive: a label on the left, a
// read value (or an edit input) paired with its inline actions in a daisyUI
// join, an optional origin badge, and an optional drill-in chevron. Every value
// list (Fields, Variables, Secrets, Tags) renders through it, so three platform
// rules are defined once and cannot drift:
//
//  - Origin treatment. An override reads with WEIGHT (emphasize); the noisy
//    default origin (empty or "default") shows NO badge; a real origin
//    ("Global", a location, "override") keeps a neutral text badge. The signal
//    is weight plus text, never colour alone (accessible), and there is no
//    edge/selection bar (an edge bar reads as "this row is selected").
//  - No editable control outside edit mode. With editing false the row renders
//    `value` only and never `input`, so read mode is pure scan, zero inputs.
//  - One inline-action family. The value/input and the `actions` buttons form
//    ONE daisyUI join, so first/last radius, whole-field :focus-within, and the
//    collapsed shared-edge hover all come from join/join-item rather than a
//    hand-rolled border group. Slot children opt into the group with `join-item`.
export default function KVRow(props: {
  // The label. Prose by default (a display name); the raw key otherwise belongs
  // in the drill-in. Consumers whose label IS the technical key with no separate
  // display name (Variables, Secrets) set `labelMono` to render it font-data.
  label: string;
  // Render the label font-data (mono), for key-style labels. Default prose.
  labelMono?: boolean;
  // Optional type badge (value_type / secret_type) shown right after the label.
  // Fields pass nothing; Variables and Secrets pass their declared type.
  typeBadge?: string;
  // Read-mode value render. Ignored in edit mode when `input` is given.
  value?: JSX.Element;
  // Edit-mode control; rendered in the join ahead of `actions`.
  input?: JSX.Element;
  // Inline action buttons (set / revert / copy / reveal / generate), in the join.
  actions?: JSX.Element;
  // Origin badge text ("override", "Global", a location). Empty or "default"
  // suppresses the badge (origin treatment).
  origin?: string;
  // Weight the label to mark an override (the scan signal, not colour).
  emphasize?: boolean;
  // Edit mode; the parent derives this from the component-detail edit context.
  editing?: boolean;
  // Opens the resolution view; a trailing chevron shows when set.
  onDrillIn?: () => void;
  // Suppress the top border on the first row of a list.
  first?: boolean;
}): JSX.Element {
  // A real origin keeps its badge; the noisy default (empty or "default") shows none.
  const showOrigin = () => {
    const o = props.origin?.trim();
    return !!o && o.toLowerCase() !== "default";
  };
  // The edit control (and its bordered box) appears only in edit mode; read mode
  // is a slim one-line scan with the value inline, no box.
  const showInput = () => !!props.editing && props.input !== undefined;

  return (
    <div
      class="flex items-center gap-2 px-3 py-2"
      classList={{ "border-t border-base-300": !props.first }}
    >
      <span
        class="min-w-0 truncate text-sm"
        classList={{ "grow basis-32": showInput(), "font-medium": props.emphasize, "font-data": props.labelMono }}
        title={props.label}
      >
        {props.label}
      </span>
      <Show when={props.typeBadge}>
        <span class="badge badge-ghost badge-sm shrink-0">{props.typeBadge}</span>
      </Show>
      <Show
        when={showInput()}
        fallback={
          <>
            {/* Read: value inline, pushed right, mono, no box (mirrors the
                Variables/Secrets rows); read-valid actions (reveal/copy) sit after it. */}
            <span class="flex-1" />
            <Show when={props.value !== undefined}>
              <span
                class="min-w-0 max-w-[60%] truncate text-right font-data text-sm text-base-content/70"
                classList={{ "font-medium text-base-content": props.emphasize }}
              >
                {props.value}
              </span>
            </Show>
            {props.actions}
          </>
        }
      >
        {/* Edit: the input and its actions become one bordered daisyUI join. */}
        <div class="join min-w-0 grow basis-64">
          {props.input}
          {props.actions}
        </div>
      </Show>
      <Show when={showOrigin()}>
        <span class="badge badge-ghost badge-sm shrink-0">{props.origin}</span>
      </Show>
      <Show when={props.onDrillIn}>
        <button
          type="button"
          class="shrink-0 text-base-content/40 hover:text-base-content"
          aria-label="Show resolution"
          onClick={() => props.onDrillIn?.()}
        >
          <ChevronRight size={14} />
        </button>
      </Show>
    </div>
  );
}
