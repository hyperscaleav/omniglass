import { type JSX } from "solid-js";

// KVStacked is the console's one key:value STACKED primitive: an eyebrow label
// above its value, the scannable detail-grid cell (Identity, Placement, file
// metadata, the entity facts). It is the sibling of KVRow (the one-line value
// row); together they are the KV primitive's two layouts. Every stacked fact
// renders through it, so the eyebrow-over-value shape is defined once and cannot
// drift: `TreeList.ctx.fact` and `DetailShell.Fact` were byte-identical copies of
// this markup and now delegate here.
export default function KVStacked(props: {
  // The label, rendered as the small-caps eyebrow above the value.
  label: string;
  // The value render. A cell whose value is not yet known passes nothing.
  value?: JSX.Element;
  // Render the value font-data (mono). Off by default; most callers style their
  // own value span, so leaving it unset preserves the plain `text-sm` value box.
  mono?: boolean;
}): JSX.Element {
  return (
    <div>
      <div class="eyebrow mb-1.5">{props.label}</div>
      <div class="text-sm" classList={{ "font-data": props.mono }}>
        {props.value}
      </div>
    </div>
  );
}
