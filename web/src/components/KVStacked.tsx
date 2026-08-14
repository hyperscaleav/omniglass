import { Show, type JSX } from "solid-js";
import ProvenanceDot from "./ProvenanceDot";
import { IDENTITY_LABELS, type IdentityBinding } from "../lib/entities";

// KVStacked is the console's one key:value STACKED primitive: an eyebrow label
// above its value, the scannable detail-grid cell (Identity, Placement, file
// metadata, the entity facts). It is the sibling of KVRow (the one-line value
// row); together they are the KV primitive's two layouts. Every stacked fact
// renders through it, so the eyebrow-over-value shape is defined once and cannot
// drift: `TreeList.ctx.fact` and `DetailShell.Fact` were byte-identical copies of
// this markup and now delegate here. It is also the READ state of BladeField, so
// a blade field and a blade fact are the same shape when nothing is being edited.
type KVStackedBase = {
  // The value render. A cell whose value is not yet known passes nothing.
  value?: JSX.Element;
  // Render the value font-data (mono). Off by default; most callers style their
  // own value span, so leaving it unset preserves the plain `text-sm` value box.
  mono?: boolean;
  // The name of the row this fact's value comes from, when it comes from
  // somewhere that is not this row; empty (or absent) marks nothing. It rides in
  // the EYEBROW, which is the same slot FieldRow gives its label, so a field
  // carries the mark in the same position whether it is being read or edited.
  // A string rather than an element, for the reason FieldRow's copy of this prop
  // records.
  provenance?: string;
};

// A fact either names one of the identity facts, taking its label from
// lib/entities, or carries a label of its own. Same rule as FieldRow and
// BladeField: the two words of the triad are written in exactly one place, so
// "Name" cannot come to mean the friendly string on one surface and the
// identifier on the next.
export type KVStackedProps =
  | (KVStackedBase & { label: string; bind?: never })
  | (KVStackedBase & { bind: IdentityBinding; label?: never });

export default function KVStacked(props: KVStackedProps): JSX.Element {
  const label = () => (props.bind ? IDENTITY_LABELS[props.bind] : props.label!);
  return (
    <div>
      <div class="eyebrow mb-1.5 flex items-center gap-1.5">
        {label()}
        <Show when={props.provenance}>
          {(from) => <ProvenanceDot label={label()} from={from()} />}
        </Show>
      </div>
      <div class="text-sm" classList={{ "font-data": props.mono }}>
        {props.value}
      </div>
    </div>
  );
}
