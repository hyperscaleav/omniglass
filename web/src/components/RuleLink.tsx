import { Show, type JSX } from "solid-js";
import { A } from "@solidjs/router";

// ruleHref is where a rule opens: the Rules page, its blade open (#951).
export const ruleHref = (name: string) => `/rules?rule=${encodeURIComponent(name)}`;

// RuleLink names the event rule an occurrence belongs to ("by rule
// reachability-lost"), so an event a rule derived on Activity leads back to
// the rule that wrote it. It links only while the rule exists (the event
// carries its id); a rule deleted since keeps the name the event recorded,
// shown as plain text that says the rule is gone, never as a dead link.
export default function RuleLink(props: { name: string; id?: string }): JSX.Element {
  return (
    <span class="shrink-0 text-[11px] text-base-content/45">
      by rule{" "}
      <Show
        when={props.id}
        fallback={
          <span class="text-base-content/55" title="This rule has since been deleted; this is the name it had when it wrote the event">
            <span class="font-data">{props.name}</span> (deleted)
          </span>
        }
      >
        <A href={ruleHref(props.name)} class="font-data text-base-content/70 hover:underline">{props.name}</A>
      </Show>
    </span>
  );
}
