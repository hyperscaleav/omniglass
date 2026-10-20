import { type JSX } from "solid-js";
import { A } from "@solidjs/router";

// ruleHref is where a rule opens: the Rules page, its blade open (#951).
export const ruleHref = (name: string) => `/rules?rule=${encodeURIComponent(name)}`;

// RuleLink names the event rule an occurrence belongs to ("by rule
// reachability-lost"), linking to it, so an event a rule derived on Activity
// leads back to the rule that wrote it.
export default function RuleLink(props: { name: string }): JSX.Element {
  return (
    <span class="shrink-0 text-[11px] text-base-content/45">
      by rule <A href={ruleHref(props.name)} class="font-data text-base-content/70 hover:underline">{props.name}</A>
    </span>
  );
}
