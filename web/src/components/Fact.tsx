import type { JSX } from "solid-js";

// Fact: a labelled value (the detail-view building block). Label is the standard
// eyebrow; value is any node.
export default function Fact(props: { label: string; children: JSX.Element }) {
  return (
    <div>
      <div class="eyebrow mb-1.5">{props.label}</div>
      <div class="text-sm">{props.children}</div>
    </div>
  );
}
