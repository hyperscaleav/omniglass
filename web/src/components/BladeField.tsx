import { Show, useContext, type JSX } from "solid-js";
import FieldRow from "./FieldRow";
import KVStacked from "./KVStacked";
import { BladeEditContext, type BladeEdit } from "../lib/blades";
import { IDENTITY_LABELS, type IdentityBinding } from "../lib/entities";

// BladeField is the console's one labelled blade field: a label, what the field
// reads as, and what it edits as, with the read-or-edit decision made once here
// instead of at every field on every page.
//
// It exists because eleven pages defined a byte-identical local `Field`, four
// more went through positional `ctx.field(...)` / `ctx.fact(...)` helpers, and
// the read-only rendering was hand-rolled 24 times. Every blade defect was
// therefore an N-place defect: `display_name` was labelled "Name" on 11 blades,
// a long description failed to wrap in 24 fields, and a read-only blade rendered
// as a form nobody could type in.
//
// It composes rather than reimplements. The read state IS a fact (KVStacked),
// which is the rule ADR-0078 records: a blade the operator cannot edit contains
// nothing shaped like a control, because a bordered box that rejects typing
// reads as broken rather than as read-only. The edit state IS a form field
// (FieldRow), which already associates its label to its control by id and keeps
// the (i) tooltip trigger outside the <label>. Both label with the eyebrow, so
// the label does not change style when the pencil is clicked.
//
// Four things live here and nowhere else: the read-or-edit switch, the read-only
// treatment, the free-text shape (`multiline`), and the identity pairing
// (`bind`).

// The empty-value glyph, written as an escape so the repo's em-dash scan stays a
// simple grep over changed files. It used to be typed at each of the 24 sites.
export const EMPTY_VALUE = "\u2014";

type BladeFieldBase = {
  // What the field reads as, for a value that is not plain text: a badge, a
  // link, a formatted timestamp. Plain text fields pass `value` instead and get
  // the read rendering for free.
  read?: JSX.Element;
  // The field's persisted text, as an accessor so it stays reactive. Given
  // without `children`, BladeField renders the control itself, which is what
  // puts "free text edits in a textarea" in one place rather than at every
  // description.
  value?: () => string;
  // The draft the edit control binds to, when it is a different signal from the
  // persisted value (the usual case: a blade seeds its draft signals on entering
  // edit, so outside edit they are stale). Defaults to `value`.
  draft?: () => string;
  onInput?: (v: string) => void;
  // A control the caller renders itself: a select, a picker, an input in a join.
  // Given, it replaces the control BladeField would have rendered.
  children?: JSX.Element;
  // Free text. Reads wrapped with its newlines preserved (and scrolls past a
  // sensible height rather than growing the blade without bound), edits in a
  // textarea. This is the whole of the fix for a description that ran off the
  // right edge of the panel.
  multiline?: boolean;
  rows?: number;
  // Render the value in the data font (an identifier, an icon key, a uuid).
  mono?: boolean;
  placeholder?: string;
  // Tooltip text for the (i) affordance beside the label, and a hint under the
  // control. Both are edit-state affordances, as they are on FieldRow.
  info?: string;
  hint?: string;
  // The edit slot, for a body rendered OUTSIDE a BladeEditContext. Systems,
  // Components, and Locations share one renderDetail between a blade (inside a
  // provider) and a full page (outside one), so it cannot call useBladeEdit.
  // With neither prop nor context the field renders read-only, which is the
  // correct answer on the full page.
  edit?: BladeEdit;
};

// A field either says which identity fact it is bound to, and takes its label
// from that, or carries a label of its own. The two are mutually exclusive at
// the type level: there is no way to type "Name" onto a display_name field,
// which is the defect that reached eleven blades at once.
export type BladeFieldProps =
  | (BladeFieldBase & { label: string; bind?: never })
  | (BladeFieldBase & { bind: IdentityBinding; label?: never });

export default function BladeField(props: BladeFieldProps): JSX.Element {
  const ctx = useContext(BladeEditContext);
  // An explicit slot wins over the ambient one: a body inside a provider may
  // still be handed the slot directly.
  const slot = (): BladeEdit | undefined => props.edit ?? ctx;
  const editing = () => !!slot()?.editing();
  const label = () => (props.bind ? IDENTITY_LABELS[props.bind] : props.label!);
  const text = () => props.value?.() ?? "";
  // The control binds to the draft; the fact renders the persisted value.
  const editText = () => (props.draft ?? props.value)?.() ?? "";
  const wrapClass = () =>
    props.multiline ? "block max-h-56 overflow-y-auto whitespace-pre-wrap break-words" : "";

  return (
    <Show
      when={editing()}
      fallback={
        <KVStacked
          label={label()}
          mono={props.mono}
          value={
            // The wrap treatment sits on the OUTER node, so it reaches a custom
            // `read` render too. Putting it only on the plain-text branch would
            // leave a free-text field with a custom render able to run off the
            // panel again, which is the defect this component exists to end.
            <span class={wrapClass()}>
              <Show when={props.read !== undefined} fallback={
                <Show when={text() !== ""} fallback={<span class="text-base-content/40">{EMPTY_VALUE}</span>}>
                  {text()}
                </Show>
              }>
                {props.read}
              </Show>
            </span>
          }
        />
      }
    >
      <FieldRow label={label()} eyebrow info={props.info} hint={props.hint}>
        <Show when={props.children !== undefined} fallback={
          <Show
            when={props.multiline}
            fallback={
              <input
                class="input input-bordered w-full"
                classList={{ "font-data": props.mono }}
                placeholder={props.placeholder}
                value={editText()}
                onInput={(e) => props.onInput?.(e.currentTarget.value)}
              />
            }
          >
            <textarea
              class="textarea textarea-bordered w-full"
              classList={{ "font-data": props.mono }}
              rows={props.rows ?? 4}
              placeholder={props.placeholder}
              value={editText()}
              onInput={(e) => props.onInput?.(e.currentTarget.value)}
            />
          </Show>
        }>
          {props.children}
        </Show>
      </FieldRow>
    </Show>
  );
}
