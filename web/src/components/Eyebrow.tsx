import { Show, type JSX } from "solid-js";
import InfoTip from "./InfoTip";

// Eyebrow: the console's section label, with the explainer as a tooltip
// rather than a paragraph (#790, the tooltips-not-prose rule). An operator
// page carries no inline explanatory prose: what a section means is a hover
// away on the (i), never a sentence in the flow. The InfoTip trigger sits
// beside the label (never inside a <label>), and without a hint no trigger
// renders at all.
export default function Eyebrow(props: { label: string; hint?: string; href?: string; class?: string; right?: JSX.Element }) {
  return (
    <div class={`flex items-center gap-1.5 ${props.class ?? ""}`}>
      <span class="eyebrow">{props.label}</span>
      <Show when={props.hint}>
        <InfoTip text={props.hint!} label={props.label} href={props.href} />
      </Show>
      <Show when={props.right}>
        <span class="ml-auto flex items-center">{props.right}</span>
      </Show>
    </div>
  );
}
