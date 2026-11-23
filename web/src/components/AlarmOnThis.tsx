import { Show, type JSX } from "solid-js";
import { A } from "@solidjs/router";
import { useMe, canFleet } from "../lib/auth";
import { alarmOnThisHref } from "../lib/rule_form";

// "Alarm on this" (#2432): from a component's metric or property, the way to a
// rule about it. It opens the new-rule form with Reads and the component filled
// in (the page's own URL params, lib/rule_form), so the operator writes only the
// condition. Offered only to a caller who may create a rule. Plain props, so any
// surface that lists a component's keys can mount it with one line.
export default function AlarmOnThis(p: { reads: string; componentId: string; label?: string }): JSX.Element {
  const me = useMe();
  return (
    <Show when={canFleet(me.data, "rule", "create")}>
      <A
        href={alarmOnThisHref(p.reads, p.componentId)}
        class="link text-[11px] text-base-content/50 hover:text-base-content"
        aria-label={`Alarm on ${p.label ?? p.reads}`}
        data-testid={`alarm-on-${p.reads}`}
      >
        Alarm on this
      </A>
    </Show>
  );
}
