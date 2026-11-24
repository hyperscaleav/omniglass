import { Show, type JSX } from "solid-js";
import { A } from "@solidjs/router";
import { calcRuleHref } from "../lib/calc_rules";
import { drawerHref } from "../lib/drawer_address";
import InfoTip from "./InfoTip";
import { writerLabel } from "../lib/catalog_usage";
import Tooltip from "./ui/Tooltip";

// ruleHref is where a rule opens: the Rules page, its blade open (#951). The
// ref is the rule's name or its uuid; the Rules page resolves either.
export const ruleHref = (ref: string) => drawerHref("event_rule", ref);

// RuleLink names the event rule an occurrence belongs to ("by rule
// reachability-lost"), so an event a rule derived on Activity leads back to
// the rule that wrote it. It links only while the rule exists (the event
// carries its id); a rule deleted since keeps the name the event recorded,
// shown as plain text that says the rule is gone, never as a dead link. The
// link carries the id, not the name: the name shown is the one the event was
// written under, which a rename since (#977) has moved, and the id has not.
export default function RuleLink(props: { name: string; id?: string }): JSX.Element {
  return (
    <span class="max-w-[16rem] shrink-0 truncate text-[11px] text-base-content/45">
      by rule{" "}
      <Show
        when={props.id}
        fallback={
          <Tooltip as="span" tabIndex={0} class="text-base-content/55" content="This rule has since been deleted; this is the name it had when it wrote the event">
            <span class="font-data">{props.name}</span> (deleted)
          </Tooltip>
        }
      >
        <A href={ruleHref(props.id!)} class="font-data text-base-content/70 hover:underline">{props.name}</A>
      </Show>
    </span>
  );
}

// CalcRuleLink names the calc rule a calculated value was derived by ("by
// calc rule room-mean", #999), linked by the rule's id, so a rename since
// leaves the link working; the name shown is the one the sample was written
// under.
export function CalcRuleLink(props: { name: string; id: string }): JSX.Element {
  return (
    <span class="shrink-0 text-[11px] text-base-content/45">
      by calc rule{" "}
      <A href={calcRuleHref(props.id)} class="font-data text-base-content/70 hover:underline">{props.name}</A>
    </span>
  );
}

// CalcWriter names the calc rule that writes a key (#1019): one enabled rule
// writes a key, and no collector may, so a metric's or property's page says
// which rule its values come from, linked by the rule's id.
export type CalcWriter = { rule: string; rule_id: string };

export function CalcWriterLine(props: { writer: CalcWriter }): JSX.Element {
  return (
    <span class="inline-flex items-center gap-1.5 text-sm" data-testid="written-by">
      <span>
        Written by calc rule{" "}
        <A href={calcRuleHref(props.writer.rule_id)} class="font-data hover:underline">{props.writer.rule}</A>
      </span>
      <InfoTip
        label="written by a calc rule"
        text="A calc rule derives this key's values on each owner it selects. One rule writes a key, and no collector may, so its values are the rule's alone."
      />
    </span>
  );
}

// CalcWriterCell is the list's compact form: the rule's name, linked.
export function CalcWriterCell(props: { writer?: CalcWriter | null }): JSX.Element {
  return (
    <Show when={props.writer}>
      {(w) => (
        <Tooltip as={A} href={calcRuleHref(w().rule_id)} class="truncate font-data text-[12px] hover:underline" content={`Written by calc rule ${w().rule}`}>
          {w().rule}
        </Tooltip>
      )}
    </Show>
  );
}

// WriterCell is the "Written by" column of the metrics and properties lists (#2430):
// the calc rule, linked, when one writes the key, else the class of writer the API
// reports (driver, outpost probe, platform), so the column says something on every
// row rather than being empty on all but a calc rule's output.
export function WriterCell(props: { row: { written_by?: CalcWriter | null; writer_class?: string } }): JSX.Element {
  const w = () => writerLabel(props.row);
  return (
    <Show when={w()}>
      {(x) => (
        <Show
          when={x().kind === "rule" ? (x() as Extract<ReturnType<typeof writerLabel>, { kind: "rule" }>) : undefined}
          fallback={<Tooltip as="span" class="truncate text-[12px] text-base-content/60" content={(x() as { title: string }).title}>{(x() as { text: string }).text}</Tooltip>}
        >
          {(rule) => <CalcWriterCell writer={{ rule: rule().rule, rule_id: rule().rule_id }} />}
        </Show>
      )}
    </Show>
  );
}

// WriterLine is the drawer's form: "Written by calc rule x" (CalcWriterLine) when a
// rule writes the key, else "Written by driver" and its class's explanation.
export function WriterLine(props: { row: { written_by?: CalcWriter | null; writer_class?: string } }): JSX.Element {
  const w = () => writerLabel(props.row);
  return (
    <Show when={w()}>
      {(x) => (
        <Show
          when={x().kind === "rule" ? (x() as Extract<ReturnType<typeof writerLabel>, { kind: "rule" }>) : undefined}
          fallback={
            <span class="inline-flex items-center gap-1.5 text-sm" data-testid="written-by">
              <span>Written by <span class="font-medium">{(x() as { text: string }).text}</span></span>
              <InfoTip label="the writer" text={(x() as { title: string }).title} />
            </span>
          }
        >
          {(rule) => <CalcWriterLine writer={{ rule: rule().rule, rule_id: rule().rule_id }} />}
        </Show>
      )}
    </Show>
  );
}
