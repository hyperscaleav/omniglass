import { For, Show, createContext, createEffect, createMemo, createSignal, useContext, type JSX } from "solid-js";
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
  opLabel,
  ruleStateKey,
  updateBody,
  updateEventRule,
  validateDraft,
  withLane,
} from "../lib/event_rules";

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
  // A link may name a rule (/rules?rule=<name>): an event row on Activity
  // points at the rule that wrote it, and lands on its blade.
  const [search] = useSearchParams();
  const linked = search.rule;
  if (typeof linked === "string" && linked) blades.push({ kind: "rule", id: linked });

  const columns: FlatColumn<EventRule>[] = [
    identityColumn<EventRule>(),
    { key: "reads", label: "Reads", width: "150px", sortVal: (r) => r.sample_key, cell: readsCell },
    {
      key: "criteria",
      label: "Criteria",
      width: "230px",
      cell: (r) => (
        <span class="flex min-w-0 max-w-44 flex-col text-[12px] leading-snug" title={`${fireWords(r, unit(r))}, ${clearWords(r, unit(r))}`}>
          <span class="truncate">{fireWords(r, unit(r))}</span>
          <span class="truncate text-base-content/50">{clearWords(r, unit(r))}</span>
        </span>
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
  const rules = useQuery(() => ({ queryKey: EVENT_RULES_KEY, queryFn: listEventRules }));
  return () => (rules.data ?? []).find((r) => r.name === name);
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
  const row = useRuleRow(p.name);
  // An open alarm opens its timeline in place, as on the component (#950).
  const timelines = createTimelineToggles();
  const unit = useMetricUnit();
  const [err, setErr] = createSignal<string | null>(null);
  const state = useQuery(() => ({
    queryKey: ruleStateKey(p.name),
    queryFn: () => getRuleState(p.name),
    refetchInterval: DETAIL_POLL_MS,
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
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Rule not found.</p>}>
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
            <div class="grid grid-cols-[auto_1fr] items-baseline gap-x-3 gap-y-1 text-sm">
              <span class="text-[12px] text-base-content/50">Fires</span>
              <span data-testid="rule-fire">{fireWords(r(), unit(r()))}</span>
              <span class="text-[12px] text-base-content/50">Clears</span>
              <span data-testid="rule-clear">{clearWords(r(), unit(r()))}</span>
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
// draws its action rail. The name is create-only. The sample key picker spans
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

  const errors = createMemo<DraftErrors>(() => validateDraft(d, p.mode));
  const valid = () => Object.keys(errors()).length === 0;
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
      const saved = p.mode === "create" ? await createEventRule(createBody(d)) : await updateEventRule(p.rule!.name, updateBody(d, p.rule!));
      await qc.invalidateQueries({ queryKey: EVENT_RULES_KEY });
      p.onSaved(saved);
    } catch (e) {
      setFormErr(describeError(e));
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
      <Show
        when={p.mode === "create"}
        fallback={<KVStacked bind="name" value={<span class="font-data">{d.name}</span>} />}
      >
        <FieldRow bind="name" info="A lowercase kebab name, e.g. too-hot. It cannot change later.">
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
      </Show>
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
      </FieldRow>
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
        </FieldRow>
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
    </form>
  );
}
