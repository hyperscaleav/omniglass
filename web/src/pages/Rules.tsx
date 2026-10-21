import { For, Show, createContext, createEffect, createMemo, createSignal, on, useContext, type JSX } from "solid-js";
import { createStore } from "solid-js/store";
import { A, useSearchParams } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import Drawer from "../components/Drawer";
import BladeTitle from "../components/BladeTitle";
import FieldRow from "../components/FieldRow";
import KVStacked from "../components/KVStacked";
import Eyebrow from "../components/Eyebrow";
import InfoTip from "../components/InfoTip";
import ExpressionField from "../components/ExpressionField";
import Button from "../components/Button";
import AlarmTimeline, { createTimelineToggles } from "../components/AlarmTimeline";
import { EVENT_TYPES_KEY, listEventTypes } from "../lib/event_types";
import SeverityBadge from "../components/SeverityBadge";
import ComponentTypeSelect from "../components/ComponentTypeSelect";
import { identityColumn } from "../components/IdentityCell";
import { Pencil, Plus, Save, Trash, X } from "../components/icons";
import { entityLabel } from "../lib/entities";
import { useFormActions } from "../lib/formactions";
import { bindSelectValue } from "../lib/selectvalue";
import { useMe, canFleet, fleetView } from "../lib/auth";
import { OFFICIAL_LOCK, registryLock } from "../lib/catalog";
import { describeError, fmtTime, rel } from "../lib/format";
import { type BladeDef, createBladeController, useBlades, useBladeEdit } from "../lib/blades";
import { useSeverity } from "../lib/severity";
import { PROPERTIES_KEY, listProperties } from "../lib/properties";
import { METRICS_KEY, listMetricTypes, type MetricRow } from "../lib/metric_types";
import { COMPONENT_TYPES_KEY, listComponentTypes } from "../lib/component_types";
import { COMPONENTS_KEY, listComponents } from "../lib/components";
import {
  type DraftErrors,
  type EventRule,
  type RuleDraft,
  type RuleLane,
  type RuleOp,
  EVENT_RULES_KEY,
  OPS_BY_LANE,
  clearWords,
  criteriaErrorWords,
  createBody,
  createEventRule,
  defaultSeverity,
  deleteEventRule,
  draftFromRule,
  emptyDraft,
  fireWords,
  getRuleState,
  judgedWords,
  listEventRules,
  lowerShort,
  opLabel,
  previewBody,
  readValue,
  ruleStateKey,
  updateBody,
  toBuilder,
  toExpression,
  updateEventRule,
  renameEventRule,
  validateDraft,
  withLane,
} from "../lib/event_rules";
import { previewEventRule, type RulePreview } from "../lib/expressions";

// Rules (Catalog > Actions > Rules, #921): the event rule registry. A rule reads
// one sample key across the components it selects and raises an alarm at its
// severity when the value meets its fire criteria for the fire sustain; it
// clears on its clear criteria (or the opposite) held for the clear sustain.
// Each row shows what it reads, its criteria in words, and its live state (open
// alarms and streaks waiting out a sustain, counted on the list rows so the
// page polls one list, not a query per rule), so the list is the teaching
// surface: the official reachability rule's counts move while a simulated
// component is unplugged. Official rules are read-only. Rules read like a
// registry, so any rule:read lists them all (the live counts cover only the
// components the caller may read); a rule's selector reaches the whole fleet,
// so every write is judged on the caller's all-scope permissions (fleetView).

// The hints, written once: each is the (i) beside the field or fact it explains,
// on the form and the detail alike.
export const RULE_HINTS = {
  reads:
    "A rule reads one sample key: a property (text, compared with is or is not) or a metric (a number, compared any way). The lane follows the registry the key is in.",
  selector:
    "The components the rule watches: those whose type falls within a listed component type, and those listed by name. Leave both empty to watch every component.",
  criteria:
    "The fire criteria open an alarm; the clear criteria close it. A rule with no clear criteria of its own clears on the opposite of its fire criteria.",
  expression:
    "An expression over the arriving sample that is true or false: value, key, instance and component (its name and type), with comparisons, and, or and a few functions. It is checked as you type against what the rule reads, and a mistake is pointed at by line and column.",
  preview:
    "Runs this draft against the last day of its key's samples, on the components you can read, without saving or writing anything: the edges it would have written and what it would hold in force now. Each sample is judged alone, so sustains are not applied.",
  sustain:
    "How long it must hold. A value has to keep meeting the criteria for this long before the alarm opens (or clears); one contrary sample starts the wait over. Blank acts at once.",
  severity:
    "The level the alarm is raised at. A level marked outage takes its component out; any other degrades it. None makes the rule informational: it records its events and raises no alarm.",
  events:
    "The events the rule writes: one when its condition comes into force (after the fire sustain), one when it leaves force (after the clear sustain), never one per sample. condition-started and condition-ended are the generic pair; a type of your own says what happened.",
  conditions:
    "A condition is in force on a component and endpoint from the moment the rule's fire criteria have held for the sustain until its clear criteria have. Every rule holds conditions, informational or not; a rule with a severity also holds an alarm open beside each.",
  open:
    "One alarm per endpoint: a component whose two endpoints both fail carries two alarms, and one recovering clears only its own.",
  pending:
    "A streak is a component waiting out a sustain: its samples point at fire or clear, and once the streak has held for the sustain the alarm opens or clears.",
  judged:
    "When the rule last judged a sample of its key arriving from a component it watches, to within 5s. A rule whose key has stopped arriving goes quiet here while its alarms stay as they were; a disabled rule judges nothing.",
  official: "Official rules ship with Omniglass and update with it, so they are read-only. Author your own beside them.",
  enabled: "A disabled rule evaluates nothing, and disabling it clears the alarms it holds open (each stays in its component's history). Enabling it again judges the newest samples at once.",
  message: "The alarm's message. Blank uses the rule's label, or its name.",
  name:
    "A lowercase kebab name, e.g. too-hot: the address an operator types and a link carries. Renaming it breaks bookmarks and external references by design; inside the platform nothing moves, since the rule's alarms, conditions and events all hold its uuid. Past events keep the name they were written under.",
  deleted:
    "Deleted since the rule was written, so the rule matches nothing by it; the rest of what it watches still applies. Remove it to tidy the rule, or leave it: saving other changes keeps it as it is.",
} as const;

const LIST_POLL_MS = 10_000;
const DETAIL_POLL_MS = 5_000;

function originBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

// The unit a metric reads in, for the criteria words ("above 40 °C").
function useMetricUnit(): (r: EventRule) => string | undefined {
  const metrics = useQuery(() => ({ queryKey: METRICS_KEY, queryFn: listMetricTypes }));
  const byName = createMemo(() => new Map((metrics.data ?? []).map((m: MetricRow) => [m.name, m.unit])));
  return (r) => (r.sample_lane === "metric" ? byName().get(r.sample_key) || undefined : undefined);
}

function readsCell(r: EventRule): JSX.Element {
  return (
    <span class="flex min-w-0 flex-col leading-snug">
      <span class="truncate font-data text-[12px]" title={r.sample_key}>{r.sample_key}</span>
      <span class="text-[11px] text-base-content/40">{r.sample_lane}</span>
    </span>
  );
}

// alarmWords is the message a rule's alarm carries: its message, else its
// label or name (the server's ruleMessage), through the one label renderer.
function alarmWords(r: EventRule): string {
  return r.message || entityLabel(r);
}

// severityCell is a rule's level, or the quiet neutral chip an informational
// rule (no severity) wears: it raises no alarm, so there is no level to show.
function severityCell(severity: string | undefined): JSX.Element {
  return severity
    ? <SeverityBadge name={severity} />
    : <span class="badge badge-ghost badge-sm" title="Records its events and raises no alarm">informational</span>;
}

// The live state at a glance, from the counts on the list row: conditions in
// force in the info hue, open alarms in the error hue, streaks in the warning
// hue, and a quiet "clear" when the rule holds nothing right now. A disabled
// rule says so first (disabling ended what it held). Its (i) says when it
// last judged a sample, which tells a quiet rule from one whose key has
// stopped arriving; the (i) stops its click so opening the hint does not
// open the row.
function RuleLive(p: { rule: EventRule }): JSX.Element {
  const open = () => p.rule.open_alarms;
  const pending = () => p.rule.pending_streaks;
  const inForce = () => p.rule.conditions_in_force;
  return (
    <span class="inline-flex flex-wrap items-center gap-1" data-live={p.rule.name}>
      <Show when={!p.rule.enabled}>
        <span class="badge badge-ghost badge-sm">disabled</span>
      </Show>
      <Show when={inForce() > 0}>
        <span class="badge badge-soft badge-info badge-sm whitespace-nowrap tabular-nums" title={`${inForce()} conditions in force`}>{inForce()} in force</span>
      </Show>
      <Show when={open() > 0}>
        <span class="badge badge-soft badge-error badge-sm whitespace-nowrap tabular-nums">{open()} open</span>
      </Show>
      <Show when={pending() > 0}>
        <span class="badge badge-soft badge-warning badge-sm whitespace-nowrap tabular-nums" title={`${pending()} waiting out a sustain`}>{pending()} pending</span>
      </Show>
      <Show when={p.rule.enabled && inForce() === 0 && open() === 0 && pending() === 0}>
        <span class="text-[12px] text-base-content/40">clear</span>
      </Show>
      <span class="inline-flex" onClick={(e) => e.stopPropagation()}>
        <InfoTip label={`when ${p.rule.name} last judged a sample`} text={judgedWords(p.rule.last_judged_at)} />
      </span>
    </span>
  );
}

// The page owns the edit Drawer: a blade's Edit closes the stack and opens it
// (a form never stacks over a blade), and saving or closing lands back on the
// rule's blade.
type RulesUi = { openEdit: (name: string) => void };
const RulesUiContext = createContext<RulesUi>();

export default function Rules(): JSX.Element {
  const me = useMe();
  const qc = useQueryClient();
  const rules = useQuery(() => ({ queryKey: EVENT_RULES_KEY, queryFn: listEventRules, refetchInterval: LIST_POLL_MS }));
  const rows = () => (rules.data ?? []).slice().sort((a, b) => a.name.localeCompare(b.name));
  const unit = useMetricUnit();
  const canCreate = () => canFleet(me.data, "rule", "create");
  const blades = createBladeController();
  const [editing, setEditing] = createSignal<string | null>(null);
  const editRow = () => (rules.data ?? []).find((r) => r.name === editing());
  const backToBlade = (name: string) => {
    setEditing(null);
    blades.push({ kind: "rule", id: name });
  };
  // A link may name a rule (/rules?rule=<name or uuid>): an event row on Activity
  // points at the rule that wrote it, and lands on its blade. The page follows
  // the param as it changes (back and forward open the rule it names), and
  // lets go of it when the blade stack closes, so a reload does not reopen a
  // blade the operator closed.
  const [search, setSearch] = useSearchParams();
  const linked = () => (typeof search.rule === "string" && search.rule ? search.rule : undefined);
  createEffect(
    on(linked, (name) => {
      if (!name) return;
      const top = blades.stack().at(-1);
      if (top?.kind === "rule" && top.id === name) return;
      blades.close();
      blades.push({ kind: "rule", id: name });
    }),
  );
  createEffect(
    on(
      () => blades.stack().length,
      (n, prev) => {
        if (n === 0 && (prev ?? 0) > 0 && linked()) setSearch({ rule: undefined });
      },
      { defer: true },
    ),
  );

  const columns: FlatColumn<EventRule>[] = [
    identityColumn<EventRule>(),
    { key: "reads", label: "Reads", width: "150px", sortVal: (r) => r.sample_key, cell: readsCell },
    {
      key: "criteria",
      label: "Criteria",
      width: "230px",
      cell: (r) => (
        <Show
          when={r.criteria_error}
          fallback={
            <span class="flex min-w-0 max-w-44 flex-col text-[12px] leading-snug" title={`${fireWords(r, unit(r))}, ${clearWords(r, unit(r))}`}>
              <span class="truncate">{fireWords(r, unit(r))}</span>
              <span class="truncate text-base-content/50">{clearWords(r, unit(r))}</span>
            </span>
          }
        >
          {(e) => (
            <span data-criteria-error={r.name} class="text-[12px] leading-snug text-error" title={`${criteriaErrorWords(e())}: ${e().message}`}>
              criteria do not compile
            </span>
          )}
        </Show>
      ),
    },
    { key: "severity", label: "Severity", width: "88px", sortVal: (r) => r.severity ?? "", cell: (r) => severityCell(r.severity) },
    { key: "live", label: "Live", width: "120px", cell: (r) => <RuleLive rule={r} /> },
    { key: "official", label: "Origin", width: "84px", sortVal: (r) => String(r.official), cell: (r) => originBadge(r.official) },
  ];

  return (
    <RulesUiContext.Provider value={{ openEdit: (name) => { blades.close(); setEditing(name); } }}>
      <div class="flex min-h-full flex-col gap-4">
        <FlatList<EventRule>
          config={{
            entity: { name: "rule", plural: "rules" },
            rows,
            loading: () => rules.isPending,
            error: () => rules.error,
            filterKeys: [
              { key: "name", type: "string", hint: "substring", get: (r) => `${entityLabel(r)} ${r.name}`, values: () => [] },
              { key: "reads", type: "string", hint: "exact", get: (r) => r.sample_key, values: () => [...new Set(rows().map((r) => r.sample_key))] },
              { key: "lane", type: "string", hint: "exact", get: (r) => r.sample_lane, values: () => ["property", "metric"] },
              { key: "severity", type: "string", hint: "exact", get: (r) => r.severity ?? "informational", values: () => [...new Set(rows().map((r) => r.severity ?? "informational"))] },
              { key: "enabled", type: "string", hint: "exact", get: (r) => (r.enabled ? "enabled" : "disabled"), values: () => ["enabled", "disabled"] },
              { key: "official", type: "string", hint: "exact", get: (r) => (r.official ? "official" : "custom"), values: () => ["official", "custom"] },
            ],
            filterPlaceholder: "filter rules by name, key, severity…",
            columns,
            empty: "No rules yet.",
            rowId: (r) => r.name,
            blades: { registry: { rule: ruleBlade }, rootKind: "rule", controller: blades },
            railExtra: () => (
              <span class="mr-1 inline-flex items-center gap-1.5 text-[12px] text-base-content/50">
                What a rule does
                <InfoTip
                  label="rules"
                  text="A rule watches one property or metric on the components it selects. When the value meets its fire criteria for its sustain, its condition comes into force: it writes an event and, when it has a severity, opens an alarm at it. It ends the same way. A rule with no severity is informational: events only, no alarm. Live counts refresh every few seconds."
                />
              </span>
            ),
            create: canCreate()
              ? { label: "New rule", can: canCreate, body: (ctx) => <RuleForm mode="create" onSaved={ctx.select} /> }
              : undefined,
          }}
        />
      </div>
      <Show when={editing() ? editRow() : undefined}>
        {(r) => (
          <Drawer open={true} onClose={() => backToBlade(r().name)} title={<span>Edit {entityLabel(r())}</span>}>
            <RuleForm
              mode="edit"
              rule={r()}
              onSaved={async (saved) => {
                await qc.invalidateQueries({ queryKey: EVENT_RULES_KEY });
                backToBlade(saved.name);
              }}
            />
          </Drawer>
        )}
      </Show>
    </RulesUiContext.Provider>
  );
}

function useRuleRow(name: string): () => EventRule | undefined {
  return useRuleLookup(name).row;
}

// useRuleLookup reads one rule from the list, and whether the list has loaded,
// so a blade says "not found" only once it knows (no flash on a cold load).
// The ref is the rule's name or its uuid: an event links its rule by uuid
// (#977), so a link written before a rename still opens the rule.
function useRuleLookup(ref: string): { row: () => EventRule | undefined; loaded: () => boolean } {
  const rules = useQuery(() => ({ queryKey: EVENT_RULES_KEY, queryFn: listEventRules }));
  return { row: () => (rules.data ?? []).find((r) => r.name === ref || r.id === ref), loaded: () => rules.isSuccess };
}

// RuleBladeTitle reads the rule's row once. Written inline as
// `row={useRuleRow(id)}`, the JSX prop compiles to a getter that called
// useRuleRow on every read, and BladeTitle reads it inside a memo: each list
// refetch re-ran the memo, minted a fresh query observer, and that observer
// refetched at once, a request storm on /event-rules while any rule's blade
// was open.
function RuleBladeTitle(p: { name: string }): JSX.Element {
  const row = useRuleRow(p.name);
  return <BladeTitle row={row} fallback={p.name} />;
}

export const ruleBlade: BladeDef = {
  Title: (p) => <RuleBladeTitle name={p.id} />,
  Body: (p) => <RuleBladeBody name={p.id} />,
};

// The selector in words: every component, or the types and components it lists.
function SelectorFacts(p: { rule: EventRule }): JSX.Element {
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
  const byId = createMemo(() => new Map((components.data ?? []).map((c) => [c.id, c])));
  const types = () => p.rule.selector?.component_types ?? [];
  const comps = () => p.rule.selector?.components ?? [];
  return (
    <Show when={types().length + comps().length > 0} fallback={<span class="text-sm text-base-content/60">Every component</span>}>
      <div class="flex flex-wrap gap-1.5">
        <For each={types()}>{(t) => <span class="badge badge-ghost badge-sm font-data" title="Component type">{t}</span>}</For>
        <For each={comps()}>
          {(id) => (
            <A href={`/components/${id}`} class="badge badge-outline badge-sm hover:underline">
              {byId().get(id) ? entityLabel(byId().get(id)!) : id}
            </A>
          )}
        </For>
      </div>
    </Show>
  );
}

// The cap on a live list in the blade: the first few rows, then a count of the
// rest, so a rule holding a whole fleet open stays a glance.
const STATE_ROWS = 8;

// StateList renders one of a rule's live lists (open alarms, pending streaks):
// each row names its component, linked by id, with where it sits (fleets reuse
// names like display-1 across places, so a bare name is ambiguous) and the
// endpoint the row is about.
function StateList<T extends { component: string; component_id: string; instance?: string }>(p: {
  items: T[];
  empty: string;
  trailing: (item: T) => JSX.Element;
  // detail renders under an item's row when it has something open there (an
  // open alarm's timeline).
  detail?: (item: T) => JSX.Element;
}): JSX.Element {
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
  const byId = createMemo(() => new Map((components.data ?? []).map((c) => [c.id, c])));
  const where = (id: string) => {
    const c = byId().get(id);
    return c?.system ?? c?.location ?? "";
  };
  const shown = () => p.items.slice(0, STATE_ROWS);
  return (
    <Show when={p.items.length > 0} fallback={<span class="text-[12px] text-base-content/40">{p.empty}</span>}>
      <ul class="flex flex-col divide-y divide-base-300 rounded-box border border-base-300">
        <For each={shown()}>
          {(it) => (
            <li class="flex flex-col gap-1.5 px-3 py-2 text-sm">
              <span class="flex items-center gap-2">
                <span class="flex min-w-0 flex-col">
                  <span class="flex min-w-0 items-baseline gap-1.5">
                    <A href={`/components/${it.component_id}`} class="truncate font-medium hover:underline">{it.component}</A>
                    <Show when={it.instance}><span class="font-data text-[11px] text-base-content/50">{it.instance}</span></Show>
                  </span>
                  <Show when={where(it.component_id)}>
                    <span class="truncate text-[11px] text-base-content/50">in {where(it.component_id)}</span>
                  </Show>
                </span>
                <span class="ml-auto flex flex-none items-center gap-2">{p.trailing(it)}</span>
              </span>
              {p.detail?.(it)}
            </li>
          )}
        </For>
      </ul>
      <Show when={p.items.length > STATE_ROWS}>
        <span class="text-[12px] text-base-content/50">and {p.items.length - STATE_ROWS} more</span>
      </Show>
    </Show>
  );
}

function RuleBladeBody(p: { name: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const ui = useContext(RulesUiContext);
  const lookup = useRuleLookup(p.name);
  const row = lookup.row;
  // An open alarm opens its timeline in place, as on the component (#950).
  const timelines = createTimelineToggles();
  const unit = useMetricUnit();
  const [err, setErr] = createSignal<string | null>(null);
  // No row, no state to read: a link naming a rule that is gone reads nothing.
  // The state reads by the rule's current name: the blade may be opened by uuid.
  const ruleName = () => row()?.name ?? p.name;
  const state = useQuery(() => ({
    queryKey: ruleStateKey(ruleName()),
    queryFn: () => getRuleState(ruleName()),
    refetchInterval: DETAIL_POLL_MS,
    enabled: !!row(),
  }));
  const canUpdate = () => canFleet(me.data, "rule", "update");
  const canDelete = () => canFleet(me.data, "rule", "delete");

  async function toggleEnabled(r: EventRule) {
    setErr(null);
    try {
      await updateEventRule(r.name, { enabled: !r.enabled });
      await qc.invalidateQueries({ queryKey: EVENT_RULES_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function remove(r: EventRule) {
    if (!confirm(`Delete rule "${r.name}"? Deleting it clears the alarms it holds open; its past alarms stay in each component's history, no longer naming it.`)) return;
    setErr(null);
    try {
      await deleteEventRule(r.name);
      blades.close();
      await qc.invalidateQueries({ queryKey: EVENT_RULES_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  // An official row is read-only for everyone: the Edit / Delete pair greys with
  // the official sentence and nothing else binds. A custom row offers Edit (the
  // Drawer), the Disable / Enable toggle and a kebab Delete, each on its own
  // permission; a caller holding none sees the pair greyed with what it needs.
  edit.bind({
    primary: () => {
      const r = row();
      return r && !r.official && canUpdate() && ui ? { label: "Edit", icon: <Pencil size={15} />, onClick: () => ui.openEdit(r.name) } : undefined;
    },
    destructive: () => {
      const r = row();
      if (!r || r.official || !canUpdate()) return undefined;
      return r.enabled
        ? { label: "Disable", tone: "warn", onClick: () => void toggleEnabled(r) }
        : { label: "Enable", tone: "ok", onClick: () => void toggleEnabled(r) };
    },
    secondary: () => {
      const r = row();
      if (!r || r.official || !canDelete()) return [];
      return [{ label: "Delete", icon: <Trash size={15} />, tone: "danger", onClick: () => void remove(r) }];
    },
    locked: () => {
      const r = row();
      if (!r) return null;
      if (r.official) return OFFICIAL_LOCK;
      // Only when the primary Edit is not bound (BladeStack refuses the pair).
      return canUpdate() ? null : registryLock(r, fleetView(me.data), "rule");
    },
  });

  return (
    <Show
      when={row()}
      fallback={
        <Show when={lookup.loaded()} fallback={<p class="text-sm text-base-content/40">Loading…</p>}>
          <p class="text-sm text-base-content/50">Rule not found.</p>
        </Show>
      }
    >
      {(r) => (
        <div class="flex flex-col gap-5">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <KVStacked bind="name" value={<span class="font-data">{r().name}</span>} />
            <KVStacked
              label="Origin"
              value={
                <span class="inline-flex items-center gap-1.5">
                  {originBadge(r().official)}
                  <Show when={r().official}><InfoTip label="official rules" text={RULE_HINTS.official} /></Show>
                </span>
              }
            />
            <KVStacked
              label="Severity"
              value={
                <span class="inline-flex items-center gap-1.5">
                  {severityCell(r().severity)}
                  <InfoTip label="severity" text={RULE_HINTS.severity} />
                  <A href="/severity-levels" class="link text-[11px] text-base-content/50 hover:text-base-content">All levels</A>
                </span>
              }
            />
            <KVStacked
              label="Status"
              value={
                <span class="inline-flex items-center gap-1.5">
                  {r().enabled
                    ? <span class="badge badge-soft badge-success badge-sm">enabled</span>
                    : <span class="badge badge-ghost badge-sm">disabled</span>}
                  <InfoTip label="enabled" text={RULE_HINTS.enabled} />
                </span>
              }
            />
            <KVStacked
              label="Last judged"
              value={
                <span class="inline-flex items-center gap-1.5">
                  <Show
                    when={state.data?.last_judged_at ?? r().last_judged_at}
                    fallback={<span class="text-base-content/50" data-testid="rule-judged">never</span>}
                  >
                    {(at) => <span class="tabular-nums" data-testid="rule-judged" title={fmtTime(at())}>{rel(at())}</span>}
                  </Show>
                  <InfoTip label="last judged" text={RULE_HINTS.judged} />
                </span>
              }
            />
          </div>
          <Show when={r().description}>
            <KVStacked label="Description" value={<span class="text-base-content/80">{r().description}</span>} />
          </Show>

          <section class="flex flex-col gap-2">
            <Eyebrow label="Reads" hint={RULE_HINTS.reads} />
            <div>{readsCell(r())}</div>
            <Eyebrow label="Watches" hint={RULE_HINTS.selector} class="mt-1" />
            <SelectorFacts rule={r()} />
          </section>

          <section class="flex flex-col gap-2">
            <Eyebrow label="Criteria" hint={RULE_HINTS.criteria} />
            <Show when={r().criteria_error}>
              {(e) => (
                <div role="alert" data-testid="rule-criteria-error" class="alert alert-error alert-soft text-sm">
                  <span>
                    The criteria do not compile ({criteriaErrorWords(e())}): <span class="font-data">{e().message}</span>. The rule
                    holds, judging nothing, until they are rewritten.
                  </span>
                </div>
              )}
            </Show>
            <div class="grid grid-cols-[auto_1fr] items-baseline gap-x-3 gap-y-1 text-sm">
              <span class="text-[12px] text-base-content/50">Fires</span>
              <span data-testid="rule-fire">{fireWords(r(), unit(r()))}</span>
              <span class="text-[12px] text-base-content/50">Clears</span>
              <span data-testid="rule-clear">{clearWords(r(), unit(r()))}</span>
            </div>
            <div class="grid grid-cols-[auto_1fr] items-baseline gap-x-3 gap-y-1">
              <span class="text-[12px] text-base-content/50">Fire</span>
              <code data-testid="rule-fire-expr" class="break-all font-data text-[12px] text-base-content/80">{r().fire_criteria.expr}</code>
              <span class="text-[12px] text-base-content/50">Clear</span>
              <code data-testid="rule-clear-expr" class="break-all font-data text-[12px] text-base-content/80">
                {r().clear_criteria?.expr ?? `!(${r().fire_criteria.expr})`}
              </code>
            </div>
            <Show when={r().message}>
              <KVStacked label="Message" value={<span class="text-base-content/80">{r().message}</span>} />
            </Show>
          </section>

          <section class="flex flex-col gap-2" aria-label="Events it writes">
            <Eyebrow label="Writes" hint={RULE_HINTS.events} />
            <div class="grid grid-cols-[auto_1fr] items-baseline gap-x-3 gap-y-1 text-sm">
              <span class="text-[12px] text-base-content/50">Starts</span>
              <span class="font-data" data-testid="rule-fire-type">{r().fire_event_type}</span>
              <span class="text-[12px] text-base-content/50">Ends</span>
              <span data-testid="rule-clear-type">
                {r().clear_event_type ? <span class="font-data">{r().clear_event_type}</span> : <span class="text-base-content/50">no clear event</span>}
              </span>
            </div>
          </section>

          <section class="flex flex-col gap-2" aria-label="Conditions in force">
            <Eyebrow
              label={`Conditions in force${state.data ? ` (${state.data.conditions.length})` : ""}`}
              hint={RULE_HINTS.conditions}
            />
            <Show when={state.data} fallback={<span class="text-[12px] text-base-content/40">Loading…</span>}>
              <StateList
                items={state.data!.conditions}
                empty="No condition in force."
                trailing={(k) => (
                  <span class="w-20 text-right text-[11px] text-base-content/50" title={fmtTime(k.since)}>since {rel(k.since)}</span>
                )}
              />
            </Show>
          </section>

          <section class="flex flex-col gap-2" aria-label="Open alarms">
            <Eyebrow
              label={`Open alarms${state.data ? ` (${state.data.open.length})` : ""}`}
              hint={RULE_HINTS.open}
            />
            <Show when={state.data} fallback={<span class="text-[12px] text-base-content/40">Loading…</span>}>
              <StateList
                items={state.data!.open}
                empty="No open alarms."
                trailing={(a) => (
                  <>
                    <SeverityBadge name={a.severity} size="xs" />
                    <span class="w-16 text-right text-[11px] text-base-content/50" title={fmtTime(a.raised_at)}>{rel(a.raised_at)}</span>
                    {timelines.button(a.alarm_id, alarmWords(r()))}
                  </>
                )}
                detail={(a) => (
                  <Show when={timelines.isOpen(a.alarm_id)}>
                    <AlarmTimeline component={a.component_id} alarmId={a.alarm_id} id={timelines.panelId(a.alarm_id)} label={timelines.label(alarmWords(r()))} />
                  </Show>
                )}
              />
            </Show>
          </section>

          <section class="flex flex-col gap-2" aria-label="Pending streaks">
            <Eyebrow
              label={`Waiting out a sustain${state.data ? ` (${state.data.pending.length})` : ""}`}
              hint={RULE_HINTS.pending}
            />
            <Show when={state.data}>
              <StateList
                items={state.data!.pending}
                empty="No streak pending."
                trailing={(s) => (
                  <>
                    <span class={`badge badge-sm ${s.pending === "fire" ? "badge-soft badge-warning" : "badge-soft badge-success"}`}>
                      {s.pending === "fire" ? "to fire" : "to clear"}
                    </span>
                    <span class="w-24 text-right text-[11px] text-base-content/50" title={`since ${fmtTime(s.since)}`}>held {rel(s.since).replace(/ ago$/, "")}</span>
                  </>
                )}
              />
            </Show>
          </section>
        </div>
      )}
    </Show>
  );
}

// ---- the form ----------------------------------------------------------------

// DeletedMark rides a selector chip whose entry no longer exists: the word, and
// the (i) saying what that means for the rule.
function DeletedMark(p: { what: string }): JSX.Element {
  return (
    <span class="inline-flex items-center gap-0.5 text-[10px] italic">
      deleted
      <InfoTip label={`deleted ${p.what}`} text={RULE_HINTS.deleted} />
    </span>
  );
}

// RuleForm authors a rule, on create and on edit alike; a Drawer hosts it and
// draws its action rail. An edit may rename (#977), on the entity forms' save
// model: the PATCH first, the :rename LAST, so a refused rename (a taken name)
// leaves the rest saved and says why. The sample key picker spans
// both registries and its lane decides the comparisons and the value's type;
// every rule is judged in TypeScript (lib/event_rules), shown beside its field
// once the field is touched, and gates the submit.
export function RuleForm(p: { mode: "create" | "edit"; rule?: EventRule; onSaved: (r: EventRule) => void }): JSX.Element {
  const qc = useQueryClient();
  const severity = useSeverity();
  const properties = useQuery(() => ({ queryKey: PROPERTIES_KEY, queryFn: listProperties }));
  const metrics = useQuery(() => ({ queryKey: METRICS_KEY, queryFn: listMetricTypes }));
  const componentTypes = useQuery(() => ({ queryKey: COMPONENT_TYPES_KEY, queryFn: listComponentTypes }));
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
  const eventTypes = useQuery(() => ({ queryKey: EVENT_TYPES_KEY, queryFn: listEventTypes }));
  const [d, setD] = createStore<RuleDraft>(p.rule ? draftFromRule(p.rule) : emptyDraft());
  // Whether the author picked a severity, None included: the create default
  // fills in only until they do.
  const [severityChosen, setSeverityChosen] = createSignal(false);
  const [touched, setTouched] = createStore<Record<string, boolean>>({});
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  // A new rule starts at the registry's default level once the levels land,
  // until its author picks one (None included). An edit never fills one in:
  // an informational rule (no severity) stays informational unless its author
  // picks a level.
  createEffect(() => {
    if (p.mode === "create" && !severityChosen() && !d.severity) {
      const s = defaultSeverity(severity().levels);
      if (s) setD("severity", s);
    }
  });

  const errors = createMemo<DraftErrors>(() => validateDraft(d));
  // The server's verdict on each criterion written as an expression (undefined
  // until a check answers): a source that does not compile refuses the submit.
  const [exprOk, setExprOk] = createStore<{ fire?: boolean; clear?: boolean }>({});
  // A side written as an expression must have compiled: pending or failed
  // checks both hold the save.
  const exprBad = () =>
    (d.fireExpr !== null && exprOk.fire !== true) || (d.clearMode === "custom" && d.clearExpr !== null && exprOk.clear !== true);
  const valid = () => Object.keys(errors()).length === 0 && !exprBad();
  const shown = (k: keyof DraftErrors) => (touched[k] ? errors()[k] : undefined);
  const touch = (k: keyof DraftErrors) => setTouched(k, true);

  const byLabel = (a: { label?: string; name: string }, b: { label?: string; name: string }) => entityLabel(a).localeCompare(entityLabel(b));
  const propertyOptions = createMemo(() => [...(properties.data ?? [])].sort(byLabel));
  const metricOptions = createMemo(() => [...(metrics.data ?? [])].sort(byLabel));
  const componentOptions = createMemo(() => [...(components.data ?? [])].filter((c) => !d.components.includes(c.id)).sort(byLabel));
  const componentById = createMemo(() => new Map((components.data ?? []).map((c) => [c.id, c])));
  // A selector entry the registries no longer hold (deleted after the rule was
  // written) is judged only once its list has landed, so nothing reads as
  // deleted while loading.
  const typeNames = createMemo(() => (componentTypes.data ? new Set(componentTypes.data.map((t) => t.name)) : undefined));
  const typeGone = (t: string) => !!typeNames() && !typeNames()!.has(t);
  const componentGone = (id: string) => !!components.data && !componentById().has(id);
  const keyValue = () => (d.sampleKey ? `${d.lane}:${d.sampleKey}` : "");
  // A property's data type types its value (a bool property compares true or
  // false), which is how the builder's comparison lowers.
  const dataType = () => (d.lane === "property" ? properties.data?.find((x) => x.name === d.sampleKey)?.data_type : undefined);
  const lowered = (side: "fire" | "clear") =>
    side === "fire"
      ? lowerShort(d.fireOp, readValue(d.fireValue, d.lane).value, d.lane, dataType())
      : lowerShort(d.clearOp, readValue(d.clearValue, d.lane).value, d.lane, dataType());
  const setMode = (side: "fire" | "clear", expression: boolean) => {
    setExprOk(side, undefined);
    const next = expression ? toExpression(d, side, dataType()) : toBuilder(d, side);
    setD({ fireExpr: next.fireExpr, fireOp: next.fireOp, fireValue: next.fireValue, fireKept: next.fireKept,
      clearExpr: next.clearExpr, clearOp: next.clearOp, clearValue: next.clearValue, clearKept: next.clearKept });
  };
  const ModeToggle = (q: { side: "fire" | "clear" }) => (
    <label class="-mt-2 inline-flex w-fit cursor-pointer items-center gap-2 text-[12px] text-base-content/70">
      <input
        type="checkbox"
        class="toggle toggle-xs"
        aria-label={`Write the ${q.side} criteria as an expression`}
        checked={(q.side === "fire" ? d.fireExpr : d.clearExpr) !== null}
        onChange={(e) => setMode(q.side, e.currentTarget.checked)}
      />
      Write as an expression
    </label>
  );
  const Lowered = (q: { side: "fire" | "clear" }) => (
    <Show when={lowered(q.side)}>
      {(x) => (
        <span class="text-[11px] text-base-content/50">
          Stored as <code data-testid={`${q.side}-lowered`} class="font-data text-base-content/70">{x()}</code>
        </span>
      )}
    </Show>
  );

  function pickKey(v: string) {
    touch("sampleKey");
    const sep = v.indexOf(":");
    if (sep < 0) return;
    setD(withLane({ ...d, componentTypes: [...d.componentTypes], components: [...d.components] }, v.slice(sep + 1), v.slice(0, sep) as RuleLane));
  }

  useFormActions().bind({
    submitLabel: p.mode === "create" ? "Create rule" : "Save rule",
    submitIcon: p.mode === "create" ? Plus : Save,
    submit: () => void submit(),
    busy,
    disabled: () => !valid(),
  });

  async function submit() {
    if (!valid()) {
      for (const k of Object.keys(errors())) touch(k as keyof DraftErrors);
      return;
    }
    setBusy(true);
    setFormErr(null);
    try {
      if (p.mode === "create") {
        const saved = await createEventRule(createBody(d));
        await qc.invalidateQueries({ queryKey: EVENT_RULES_KEY });
        p.onSaved(saved);
        return;
      }
      const was = p.rule!;
      const to = d.name.trim();
      const body = updateBody(d, was);
      let saved = was;
      // A rename alone sends no PATCH: there is nothing else to write.
      if (to === was.name || Object.keys(body).length > 0) saved = await updateEventRule(was.name, body);
      if (to !== was.name) saved = await renameEventRule(was.name, to);
      await qc.invalidateQueries({ queryKey: EVENT_RULES_KEY });
      p.onSaved(saved);
    } catch (e) {
      setFormErr(describeError(e));
      // A PATCH that landed before a refused rename is saved: show it.
      if (p.mode === "edit") await qc.invalidateQueries({ queryKey: EVENT_RULES_KEY });
    } finally {
      setBusy(false);
    }
  }

  const err = (k: keyof DraftErrors) => (
    <Show when={shown(k)}>{(m) => <p class="text-[11px] text-error">{m()}</p>}</Show>
  );

  const OpSelect = (q: { value: RuleOp; onChange: (op: RuleOp) => void; label: string }) => (
    <select
      aria-label={q.label}
      ref={bindSelectValue(() => q.value, () => d.lane)}
      class="select select-bordered w-40 flex-none"
      onChange={(e) => q.onChange(e.currentTarget.value as RuleOp)}
    >
      <For each={OPS_BY_LANE[d.lane]}>{(op) => <option value={op}>{opLabel(op, d.lane)}</option>}</For>
    </select>
  );

  return (
    <form class="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <Show when={formErr()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{formErr()}</span></div>
      </Show>
      <FieldRow bind="name" info={RULE_HINTS.name}>
        <input
          class="input input-bordered w-full font-data"
          classList={{ "input-error": !!shown("name") }}
          aria-required="true"
          value={d.name}
          placeholder="too-hot"
          onInput={(e) => { setD("name", e.currentTarget.value); touch("name"); }}
        />
        {err("name")}
      </FieldRow>
      <FieldRow bind="label">
        <input class="input input-bordered w-full" value={d.label} placeholder="Too hot" onInput={(e) => setD("label", e.currentTarget.value)} />
      </FieldRow>

      <FieldRow label="Reads" info={RULE_HINTS.reads}>
        <select
          ref={bindSelectValue(keyValue, propertyOptions, metricOptions)}
          class="select select-bordered w-full"
          classList={{ "select-error": !!shown("sampleKey") }}
          aria-required="true"
          onChange={(e) => pickKey(e.currentTarget.value)}
        >
          <option value="" disabled>Choose a property or metric…</option>
          <optgroup label="Properties">
            <For each={propertyOptions()}>{(r) => <option value={`property:${r.name}`}>{entityLabel(r)} ({r.name})</option>}</For>
          </optgroup>
          <optgroup label="Metrics">
            <For each={metricOptions()}>{(m) => <option value={`metric:${m.name}`}>{entityLabel(m)} ({m.name})</option>}</For>
          </optgroup>
        </select>
        {err("sampleKey")}
      </FieldRow>

      <Show
        when={d.fireExpr !== null}
        fallback={
          <FieldRow label="Fires when it reads" info={RULE_HINTS.criteria}>
            <div class="flex gap-2">
              <OpSelect label="Fire comparison" value={d.fireOp} onChange={(op) => setD("fireOp", op)} />
              <input
                aria-label="Fire value"
                class="input input-bordered min-w-0 flex-1 font-data"
                classList={{ "input-error": !!shown("fireValue") }}
                inputmode={d.lane === "metric" ? "decimal" : "text"}
                placeholder={d.lane === "metric" ? "40" : "down"}
                value={d.fireValue}
                onInput={(e) => { setD("fireValue", e.currentTarget.value); touch("fireValue"); }}
              />
            </div>
            {err("fireValue")}
            <Lowered side="fire" />
          </FieldRow>
        }
      >
        <FieldRow label="Fires when" info={RULE_HINTS.expression}>
          <ExpressionField
            site="criteria"
            sampleKey={d.sampleKey}
            label="Fire expression"
            placeholder={'value == "down"'}
            value={d.fireExpr ?? ""}
            onInput={(v) => { setD("fireExpr", v); touch("fireExpr"); }}
            onCheck={(ok) => setExprOk("fire", ok)}
          />
          {err("fireExpr")}
        </FieldRow>
      </Show>
      <ModeToggle side="fire" />
      <FieldRow label="For at least" info={RULE_HINTS.sustain}>
        <input
          class="input input-bordered w-full font-data"
          classList={{ "input-error": !!shown("fireSustain") }}
          placeholder="15s"
          value={d.fireSustain}
          onInput={(e) => { setD("fireSustain", e.currentTarget.value); touch("fireSustain"); }}
        />
        {err("fireSustain")}
      </FieldRow>

      <FieldRow label="Clears" info={RULE_HINTS.criteria}>
        <select
          ref={bindSelectValue(() => d.clearMode, () => true)}
          class="select select-bordered w-full"
          onChange={(e) => setD("clearMode", e.currentTarget.value as RuleDraft["clearMode"])}
        >
          <option value="opposite">On the opposite of the fire criteria</option>
          <option value="custom">On criteria of its own</option>
        </select>
      </FieldRow>
      <Show when={d.clearMode === "custom"}>
        <Show
          when={d.clearExpr !== null}
          fallback={
            <FieldRow label="Clears when it reads">
              <div class="flex gap-2">
                <OpSelect label="Clear comparison" value={d.clearOp} onChange={(op) => setD("clearOp", op)} />
                <input
                  aria-label="Clear value"
                  class="input input-bordered min-w-0 flex-1 font-data"
                  classList={{ "input-error": !!shown("clearValue") }}
                  inputmode={d.lane === "metric" ? "decimal" : "text"}
                  placeholder={d.lane === "metric" ? "35" : "up"}
                  value={d.clearValue}
                  onInput={(e) => { setD("clearValue", e.currentTarget.value); touch("clearValue"); }}
                />
              </div>
              {err("clearValue")}
              <Lowered side="clear" />
            </FieldRow>
          }
        >
          <FieldRow label="Clears when" info={RULE_HINTS.expression}>
            <ExpressionField
              site="criteria"
              sampleKey={d.sampleKey}
              label="Clear expression"
              placeholder={'value == "up"'}
              value={d.clearExpr ?? ""}
              onInput={(v) => { setD("clearExpr", v); touch("clearExpr"); }}
              onCheck={(ok) => setExprOk("clear", ok)}
            />
            {err("clearExpr")}
          </FieldRow>
        </Show>
        <ModeToggle side="clear" />
      </Show>
      <FieldRow label="Clears after at least" info={RULE_HINTS.sustain}>
        <input
          class="input input-bordered w-full font-data"
          classList={{ "input-error": !!shown("clearSustain") }}
          placeholder="0s"
          value={d.clearSustain}
          onInput={(e) => { setD("clearSustain", e.currentTarget.value); touch("clearSustain"); }}
        />
        {err("clearSustain")}
      </FieldRow>

      <FieldRow label="Severity" info={RULE_HINTS.severity}>
        <select
          ref={bindSelectValue(() => d.severity, () => severity().levels)}
          class="select select-bordered w-full"
          onChange={(e) => { setSeverityChosen(true); setD("severity", e.currentTarget.value); }}
        >
          <For each={[...severity().levels].reverse()}>
            {(l) => <option value={l.name}>{entityLabel(l)}{l.outage ? " (outage)" : ""}</option>}
          </For>
          <option value="">None (informational)</option>
        </select>
      </FieldRow>

      <FieldRow label="Fire event type" info={RULE_HINTS.events}>
        <select
          ref={bindSelectValue(() => d.fireEventType, () => eventTypes.data)}
          class="select select-bordered w-full font-data"
          classList={{ "select-error": !!shown("fireEventType") }}
          onChange={(e) => { setD("fireEventType", e.currentTarget.value); touch("fireEventType"); }}
        >
          <For each={eventTypes.data ?? []}>{(t) => <option value={t.name}>{t.name}</option>}</For>
        </select>
        {err("fireEventType")}
      </FieldRow>
      <FieldRow label="Clear event type" info={RULE_HINTS.events}>
        <select
          ref={bindSelectValue(() => d.clearEventType, () => eventTypes.data)}
          class="select select-bordered w-full font-data"
          onChange={(e) => setD("clearEventType", e.currentTarget.value)}
        >
          <For each={eventTypes.data ?? []}>{(t) => <option value={t.name}>{t.name}</option>}</For>
          <option value="">No clear event</option>
        </select>
      </FieldRow>

      <FieldRow label="Watches component types" info={RULE_HINTS.selector}>
        <ComponentTypeSelect
          types={componentTypes.data ?? []}
          value=""
          emptyLabel={d.componentTypes.length ? "Add another component type…" : "Add a component type…"}
          onChange={(v) => { if (v && !d.componentTypes.includes(v)) setD("componentTypes", [...d.componentTypes, v]); }}
        />
      </FieldRow>
      <FieldRow label="Watches components">
        <select
          ref={bindSelectValue(() => "", componentOptions)}
          class="select select-bordered w-full"
          onChange={(e) => { const v = e.currentTarget.value; if (v) setD("components", [...d.components, v]); e.currentTarget.value = ""; }}
        >
          <option value="">{d.components.length ? "Add another component…" : "Add a component…"}</option>
          <For each={componentOptions()}>{(c) => <option value={c.id}>{entityLabel(c)}</option>}</For>
        </select>
      </FieldRow>
      <div class="-mt-2 flex flex-wrap items-center gap-1.5" aria-label="Selected">
        <Show when={d.componentTypes.length + d.components.length > 0} fallback={<span class="text-[12px] text-base-content/50">Every component</span>}>
          <For each={d.componentTypes}>
            {(t) => (
              <span class="badge badge-ghost badge-sm gap-1 font-data" classList={{ "text-base-content/50": typeGone(t) }}>
                {t}
                <Show when={typeGone(t)}><DeletedMark what={`component type ${t}`} /></Show>
                <button type="button" class="opacity-60 hover:opacity-100" aria-label={`Stop watching ${t}`} onClick={() => setD("componentTypes", d.componentTypes.filter((x) => x !== t))}>
                  <X size={11} />
                </button>
              </span>
            )}
          </For>
          <For each={d.components}>
            {(id) => (
              <span class="badge badge-outline badge-sm gap-1" classList={{ "badge-ghost font-data text-base-content/50": componentGone(id) }}>
                {componentById().get(id) ? entityLabel(componentById().get(id)!) : id}
                <Show when={componentGone(id)}><DeletedMark what={`component ${id}`} /></Show>
                <button type="button" class="opacity-60 hover:opacity-100" aria-label={`Stop watching ${componentById().get(id)?.name ?? id}`} onClick={() => setD("components", d.components.filter((x) => x !== id))}>
                  <X size={11} />
                </button>
              </span>
            )}
          </For>
        </Show>
      </div>

      <FieldRow label="Message" info={RULE_HINTS.message}>
        <input class="input input-bordered w-full" value={d.message} placeholder={d.name ? entityLabel(d) : "Too hot"} onInput={(e) => setD("message", e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Description">
        <input class="input input-bordered w-full" value={d.description} onInput={(e) => setD("description", e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Enabled" info={RULE_HINTS.enabled}>
        <input type="checkbox" class="toggle toggle-sm" checked={d.enabled} onChange={(e) => setD("enabled", e.currentTarget.checked)} />
      </FieldRow>

      <RulePreviewPanel
        draft={d}
        ready={!!d.sampleKey && !errors().fireValue && !errors().fireExpr && !errors().clearValue && !errors().clearExpr && !exprBad()}
      />
    </form>
  );
}

// RulePreviewPanel runs the draft as a candidate rule against the last day of
// its key's samples (POST /event-rules:preview, #959): the series it would hold
// in force now, linked to their components, and the edges it would have
// written. It writes nothing, and judges each sample alone (sustains are not
// applied), which it says.
function RulePreviewPanel(p: { draft: RuleDraft; ready: boolean }): JSX.Element {
  const [result, setResult] = createSignal<RulePreview | null>(null);
  const [busy, setBusy] = createSignal(false);
  const [failed, setFailed] = createSignal<string | null>(null);
  let section: HTMLElement | undefined;
  // An answer is to the draft it was asked about: once that changes, it goes.
  createEffect(on(() => JSON.stringify(previewBody(p.draft)), () => setResult(null), { defer: true }));
  async function run() {
    setBusy(true);
    setFailed(null);
    try {
      setResult(await previewEventRule(previewBody(p.draft)));
      // The answer lands at the foot of a long drawer: bring the panel up so
      // it is read, not scrolled for.
      section?.scrollIntoView?.({ block: "start", behavior: "smooth" });
    } catch (e) {
      setResult(null);
      setFailed(describeError(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section ref={section} aria-label="Preview" class="flex flex-col gap-2 border-t border-base-300 pt-3">
      <Eyebrow
        label="Preview"
        hint={RULE_HINTS.preview}
        right={
          <Button intent="quiet" size="xs" loading={busy()} disabled={!p.ready} onClick={() => void run()}>
            Preview
          </Button>
        }
      />
      <Show when={failed()}>{(m) => <div role="alert" class="alert alert-error alert-soft text-sm"><span>{m()}</span></div>}</Show>
      <Show when={result()}>
        {(r) => (
          <div data-testid="preview-result" class="flex flex-col gap-2 text-sm">
            <Show when={r().truncated}>
              <div data-testid="preview-truncated" class="alert alert-warning alert-soft text-[12px]">
                <span>
                  Judged {r().series_judged} of {r().series_total} series: the rest would pass the{" "}
                  {r().row_cap.toLocaleString("en-US")}-sample cap.
                </span>
              </div>
            </Show>
            <Show when={(r().state_unknown ?? []).length > 0}>
              <span data-testid="preview-unknown" class="inline-flex items-center gap-1 text-[11px] text-base-content/60">
                {r().state_unknown!.length} series started unknown
                <InfoTip
                  label="series that started unknown"
                  text={`No sample in the week before the window fired or cleared, so where they started is not known: their first one in the window set their state without an edge. ${(r().state_unknown ?? []).map((s) => `${s.component}${s.instance ? ` ${s.instance}` : ""}`).join(", ")}`}
                />
              </span>
            </Show>
            <Eyebrow label={`In force now (${r().in_force_now?.length ?? 0})`} />
            <Show when={(r().in_force_now ?? []).length > 0} fallback={<span class="text-[12px] text-base-content/50">Nothing in force.</span>}>
              <ul class="flex flex-col gap-0.5">
                <For each={r().in_force_now ?? []}>
                  {(s) => (
                    <li class="flex items-center gap-2 text-[12px]">
                      <A href={`/components/${s.component_id}`} class="link link-hover">{s.component}</A>
                      <Show when={s.instance}><span class="font-data text-base-content/60">{s.instance}</span></Show>
                      <span class="ml-auto text-[11px] text-base-content/50">
                        {s.since ? <span title={fmtTime(s.since)}>since {rel(s.since)}</span> : "since before the window"}
                      </span>
                    </li>
                  )}
                </For>
              </ul>
            </Show>
            <Eyebrow label={`Edges in the last ${r().window === "24h0m0s" ? "24h" : r().window} (${r().edges?.length ?? 0})`} />
            <Show when={(r().edges ?? []).length > 0} fallback={<span class="text-[12px] text-base-content/50">No edge.</span>}>
              <ul class="flex max-h-48 flex-col gap-0.5 overflow-y-auto">
                <For each={r().edges ?? []}>
                  {(e) => (
                    <li data-testid="preview-edge" class="flex items-center gap-2 text-[12px]">
                      <span class={`badge badge-sm ${e.kind === "fire" ? "badge-soft badge-warning" : "badge-soft badge-success"}`}>{e.kind}</span>
                      <span>{e.component}</span>
                      <Show when={e.instance}><span class="font-data text-base-content/60">{e.instance}</span></Show>
                      <span class="ml-auto text-[11px] text-base-content/50" title={fmtTime(e.at)}>{rel(e.at)}</span>
                    </li>
                  )}
                </For>
              </ul>
            </Show>
            <span class="text-[11px] text-base-content/50">Sustains are ignored: each sample is judged alone.</span>
          </div>
        )}
      </Show>
    </section>
  );
}
