import { For, Index, Show, createContext, createEffect, createMemo, createSignal, createUniqueId, on, useContext, type JSX } from "solid-js";
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
import { PROPERTIES_KEY, PROPERTY_DATA_TYPES, type PropertyDataType, type PropertyRow, createProperty, listProperties } from "../lib/properties";
import { METRICS_KEY, type MetricRow, createMetricType, listMetricTypes } from "../lib/metric_types";
import { COMPONENT_TYPES_KEY, listComponentTypes } from "../lib/component_types";
import { COMPONENTS_KEY, listComponents } from "../lib/components";
import { SYSTEM_TYPES_KEY, listSystemTypes } from "../lib/system_types";
import { SYSTEMS_KEY, listSystems } from "../lib/systems";
import {
  type CalcDraft,
  type CalcOwnerState,
  type CalcPreview,
  type CalcRule,
  type OwnerKind,
  CALC_RULES_KEY,
  boundInputs,
  calcStateKey,
  calcTemplates,
  createCalcBody,
  createCalcRule,
  deleteCalcRule,
  draftFromCalcRule,
  emptyCalcDraft,
  getCalcState,
  hasErrors,
  hiddenWords,
  listCalcRules,
  liveWords,
  previewCalcBody,
  previewCalcRule,
  renameCalcRule,
  updateCalcBody,
  updateCalcRule,
  validateCalcDraft,
  withInputKey,
} from "../lib/calc_rules";

// Calc rules (Catalog > Actions > Calc rules, #999, #1018; epic #525): the
// calc rule registry, a sibling of Rules and Severity in the catalog's Actions
// group, as each registry there is its own page. A calc rule derives one
// output key on each owner it selects (a component, or a system from its
// role-bound members) by an expression over its inputs, each bound by an
// alias as the list of its current values; the server's tick computes it and
// writes the result as a calculated sample. Each row reads its reduce (the
// expression, as code) and its live counts (owners computed, pending, without
// inputs, failing, over the owners the caller may read); a row opens a blade
// with the state per owner, in the server's order; New and Edit open the one
// form in a Drawer: the inputs (a key, its alias pre-filled), the expression
// editor with start-from templates, and a preview that computes the draft now
// without saving. Writing a rule needs an all-scope grant (fleetView), as for
// event rules.

export const CALC_HINTS = {
  owner:
    "What the rule derives a value on: each component it selects, or each system, reading the system's role-bound members (the components assigned to its roles). Fixed when the rule is created.",
  selector:
    "The owners the rule derives on: those whose type falls within a listed type (subtypes included), and those listed by name. Leave both empty for every owner of its kind.",
  inputs:
    "The keys the rule reads, each by an alias its expression uses: picking a key fills the alias from its name, and you may shorten it (t, call, people). A key is a property or a metric, held by its id, so renaming it changes nothing here; health is never an input.",
  reduce:
    "An expression over the inputs' aliases. Each alias is the list of the input's current values: one per instance on a component (one value when it has one), every member's on a system. Fold a list with max, min, avg, sum, len, majority, any or all; on a system, members is one record per member, each with value.<alias> and has.<alias>.",
  output:
    "The property or metric the rule writes, on each owner, as a calculated sample naming the rule. Not one a collection writes, and not one another enabled rule writes; its type is the type the reduce must yield.",
  state:
    "What the rule last did on each owner you can read: the value it wrote, what it found no value of, the members it skipped, and whether the owner is waiting for the next tick (about a second).",
  preview:
    "Computes this draft now on the owners it selects that you can read, with the same reduce the server runs, without saving or writing anything: the value it would write, and whether that differs from what the owner holds.",
  enabled: "A disabled rule computes nothing and keeps nothing pending; its last values stay on its owners. Enabling it recomputes every owner it selects.",
  official: "Official rules ship with Omniglass and update with it, so they are read-only. Author your own beside them.",
  name:
    "A lowercase kebab name, e.g. busy-flag: the address an operator types and a link carries. Renaming it breaks bookmarks and external references; inside the platform nothing moves, and past samples keep the name they were written under.",
  binding:
    "A calc rule is its output's one writer: two writers would mix their values on an owner by whichever came last, so a key another enabled rule writes, or one a collection writes, is refused.",
  noMembers: "This system has no role-bound members, so the rule writes nothing: the last value it wrote stays, unsupported, until a member returns.",
} as const;

const LIST_POLL_MS = 10_000;
const DETAIL_POLL_MS = 5_000;

function originBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

function ownerBadge(kind: OwnerKind): JSX.Element {
  return <span class="badge badge-ghost badge-sm">{kind}</span>;
}

function inputsCell(r: CalcRule): JSX.Element {
  return (
    <span class="flex min-w-0 flex-col leading-snug" title={r.inputs.map((i) => `${i.alias} = ${i.key}`).join(", ")}>
      <For each={r.inputs.slice(0, 2)}>
        {(i) => (
          <span class="truncate text-[12px]">
            <span class="font-data text-base-content/50">{i.alias}</span> <span class="font-data">{i.key}</span>
          </span>
        )}
      </For>
      <Show when={r.inputs.length > 2}>
        <span class="text-[11px] text-base-content/40">and {r.inputs.length - 2} more</span>
      </Show>
    </span>
  );
}

function ReduceCell(p: { rule: CalcRule }): JSX.Element {
  return (
    <Show
      when={!p.rule.calc_error}
      fallback={
        <span data-calc-error={p.rule.name} class="text-[12px] leading-snug text-error" title={p.rule.calc_error!.message}>
          reduce does not compile
        </span>
      }
    >
      <code class="truncate font-data text-[12px] [font-variant-ligatures:none]" title={p.rule.reduce.expr}>{p.rule.reduce.expr}</code>
    </Show>
  );
}

// The live state at a glance, from the counts on the list row: computed owners
// in the success hue, pending in info, owners it wrote nothing for in warning,
// failing owners in error; a disabled rule says so first.
function CalcLive(p: { rule: CalcRule }): JSX.Element {
  const r = () => p.rule;
  return (
    <span class="inline-flex flex-wrap items-center gap-1" data-live={r().name} title={liveWords(r())}>
      <Show when={!r().enabled}>
        <span class="badge badge-ghost badge-sm">disabled</span>
      </Show>
      <Show when={r().owners_computed > 0}>
        <span class="badge badge-soft badge-success badge-sm whitespace-nowrap tabular-nums">{r().owners_computed} computed</span>
      </Show>
      <Show when={r().owners_pending > 0}>
        <span class="badge badge-soft badge-info badge-sm whitespace-nowrap tabular-nums">{r().owners_pending} pending</span>
      </Show>
      <Show when={r().owners_unsupported > 0}>
        <span class="badge badge-soft badge-warning badge-sm whitespace-nowrap tabular-nums">{r().owners_unsupported} without inputs</span>
      </Show>
      <Show when={r().owners_failed > 0}>
        <span class="badge badge-soft badge-error badge-sm whitespace-nowrap tabular-nums">{r().owners_failed} failing</span>
      </Show>
      <Show when={r().enabled && r().owners_computed + r().owners_pending + r().owners_unsupported + r().owners_failed === 0}>
        <span class="text-[12px] text-base-content/40">nothing yet</span>
      </Show>
    </span>
  );
}

type CalcUi = { openEdit: (name: string) => void };
const CalcUiContext = createContext<CalcUi>();

export default function CalcRules(): JSX.Element {
  const me = useMe();
  const qc = useQueryClient();
  const rules = useQuery(() => ({ queryKey: CALC_RULES_KEY, queryFn: listCalcRules, refetchInterval: LIST_POLL_MS }));
  const rows = () => (rules.data ?? []).slice().sort((a, b) => a.name.localeCompare(b.name));
  const canCreate = () => canFleet(me.data, "rule", "create");
  const blades = createBladeController();
  const [editing, setEditing] = createSignal<string | null>(null);
  const editRow = () => (rules.data ?? []).find((r) => r.name === editing());
  const backToBlade = (name: string) => {
    setEditing(null);
    blades.push({ kind: "calc-rule", id: name });
  };
  // A link may name a rule (/calc-rules?rule=<name or uuid>): a calculated
  // value on a component or a system points at the rule that derived it.
  const [search, setSearch] = useSearchParams();
  const linked = () => (typeof search.rule === "string" && search.rule ? search.rule : undefined);
  createEffect(
    on(linked, (ref) => {
      if (!ref) return;
      const top = blades.stack().at(-1);
      if (top?.kind === "calc-rule" && top.id === ref) return;
      blades.close();
      blades.push({ kind: "calc-rule", id: ref });
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

  const columns: FlatColumn<CalcRule>[] = [
    identityColumn<CalcRule>(),
    { key: "owner", label: "On", width: "90px", sortVal: (r) => r.owner_kind, cell: (r) => ownerBadge(r.owner_kind) },
    { key: "inputs", label: "Reads", width: "160px", sortVal: (r) => r.inputs[0]?.key ?? "", cell: inputsCell },
    { key: "reduce", label: "Reduce", width: "180px", cell: (r) => <ReduceCell rule={r} /> },
    { key: "output", label: "Writes", width: "130px", sortVal: (r) => r.output_key, cell: (r) => <span class="truncate font-data text-[12px]">{r.output_key}</span> },
    { key: "live", label: "Live", width: "150px", cell: (r) => <CalcLive rule={r} /> },
    { key: "official", label: "Origin", width: "84px", sortVal: (r) => String(r.official), cell: (r) => originBadge(r.official) },
  ];

  return (
    <CalcUiContext.Provider value={{ openEdit: (name) => { blades.close(); setEditing(name); } }}>
      <div class="flex min-h-full flex-col gap-4">
        <FlatList<CalcRule>
          config={{
            entity: { name: "calc rule", plural: "calc rules" },
            rows,
            loading: () => rules.isPending,
            error: () => rules.error,
            filterKeys: [
              { key: "name", type: "string", hint: "substring", get: (r) => `${entityLabel(r)} ${r.name}`, values: () => [] },
              { key: "on", type: "string", hint: "exact", get: (r) => r.owner_kind, values: () => ["component", "system"] },
              { key: "writes", type: "string", hint: "exact", get: (r) => r.output_key, values: () => [...new Set(rows().map((r) => r.output_key))] },
              { key: "enabled", type: "string", hint: "exact", get: (r) => (r.enabled ? "enabled" : "disabled"), values: () => ["enabled", "disabled"] },
            ],
            filterPlaceholder: "filter calc rules by name, output…",
            columns,
            empty: "No calc rules yet. A calc rule derives a value from others: whether a component is busy from its call state and occupancy, a system's average temperature from its members.",
            rowId: (r) => r.name,
            blades: { registry: { "calc-rule": calcRuleBlade }, rootKind: "calc-rule", controller: blades },
            railExtra: () => (
              <span class="mr-1 inline-flex items-center gap-1.5 text-[12px] text-base-content/50">
                What a calc rule does
                <InfoTip
                  label="calc rules"
                  text="A calc rule derives a value from others: it reads named inputs on each component it selects, or on each system's role-bound members, folds them into one value, and writes it as a calculated sample of its output key, which event rules then judge like any other. It recomputes within about a second of an input changing. Live counts refresh every few seconds."
                />
              </span>
            ),
            create: canCreate()
              ? { label: "New rule", can: canCreate, body: (ctx) => <CalcRuleForm mode="create" onSaved={(r) => ctx.select(r)} /> }
              : undefined,
          }}
        />
      </div>
      <Show when={editing() ? editRow() : undefined}>
        {(r) => (
          <Drawer open={true} onClose={() => backToBlade(r().name)} title={<span>Edit {entityLabel(r())}</span>}>
            <CalcRuleForm
              mode="edit"
              rule={r()}
              onSaved={async (saved) => {
                await qc.invalidateQueries({ queryKey: CALC_RULES_KEY });
                backToBlade(saved.name);
              }}
            />
          </Drawer>
        )}
      </Show>
    </CalcUiContext.Provider>
  );
}

// useCalcLookup reads one rule from the list by name or uuid, and whether the
// list has loaded, so a blade says "not found" only once it knows.
function useCalcLookup(ref: string): { row: () => CalcRule | undefined; loaded: () => boolean } {
  const rules = useQuery(() => ({ queryKey: CALC_RULES_KEY, queryFn: listCalcRules }));
  return { row: () => (rules.data ?? []).find((r) => r.name === ref || r.id === ref), loaded: () => rules.isSuccess };
}

// The title reads the row once (see Rules' RuleBladeTitle on why it is not
// inlined as a prop).
function CalcBladeTitle(p: { name: string }): JSX.Element {
  const row = useCalcLookup(p.name).row;
  return <BladeTitle row={row} fallback={p.name} />;
}

export const calcRuleBlade: BladeDef = {
  Title: (p) => <CalcBladeTitle name={p.id} />,
  Body: (p) => <CalcBladeBody name={p.id} />,
};

// The selector in words: every owner of its kind, or the types and owners it
// lists.
function SelectorFacts(p: { rule: CalcRule }): JSX.Element {
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
  const systems = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems }));
  const sys = () => p.rule.owner_kind === "system";
  const types = () => (sys() ? p.rule.selector?.system_types : p.rule.selector?.component_types) ?? [];
  const ids = () => (sys() ? p.rule.selector?.systems : p.rule.selector?.components) ?? [];
  const nameOf = (id: string) => {
    const hit = sys() ? (systems.data ?? []).find((s) => s.id === id) : (components.data ?? []).find((c) => c.id === id);
    return hit ? entityLabel(hit) : id;
  };
  return (
    <Show when={types().length + ids().length > 0} fallback={<span class="text-sm text-base-content/60">{sys() ? "Every system" : "Every component"}</span>}>
      <div class="flex flex-wrap gap-1.5">
        <For each={types()}>{(t) => <span class="badge badge-ghost badge-sm font-data" title={sys() ? "System type" : "Component type"}>{t}</span>}</For>
        <For each={ids()}>
          {(id) => <A href={`/${sys() ? "systems" : "components"}/${id}`} class="badge badge-outline badge-sm hover:underline">{nameOf(id)}</A>}
        </For>
      </div>
    </Show>
  );
}

// The most skipped members named per key on a state row; the rest counted.
const SKIPPED_NAMES = 4;
// The owners a blade lists at first, in the server's order (failing,
// pending, missing, valued); "and N more" asks the route for the rest, up to
// its most.
const STATE_ROWS = 12;
const STATE_ALL = 1000;

function skippedByKey(ms: CalcOwnerState["skipped"]): { key: string; members: CalcOwnerState["skipped"] }[] {
  const out: { key: string; members: CalcOwnerState["skipped"] }[] = [];
  for (const m of ms) {
    const g = out.find((x) => x.key === m.key);
    if (g) g.members.push(m);
    else out.push({ key: m.key, members: [m] });
  }
  return out;
}

function stateValue(v: unknown): string {
  return v === undefined || v === null ? "" : typeof v === "string" ? v : JSON.stringify(v);
}

// StateRow is one owner's last outcome: its value and when, or why it wrote
// nothing (missing inputs, no members, an error), the members it skipped (named
// when readable, counted when not), and whether it is pending.
function StateRow(p: { o: CalcOwnerState }): JSX.Element {
  const href = () => `/${p.o.owner_kind === "system" ? "systems" : "components"}/${p.o.owner_id}`;
  return (
    <li class="flex flex-col gap-1 px-3 py-2 text-sm" data-owner={p.o.owner}>
      <span class="flex items-baseline gap-2">
        <A href={href()} class="truncate font-medium hover:underline">{p.o.owner}</A>
        <Show when={p.o.value !== undefined && p.o.value !== null}>
          <span class="font-data text-[12px]">{stateValue(p.o.value)}</span>
        </Show>
        <span class="ml-auto flex flex-none items-center gap-2">
          <Show when={p.o.pending}>
            <span class="badge badge-soft badge-info badge-sm" title={p.o.due_at ? `due ${fmtTime(p.o.due_at)}` : "computes on the next tick"}>pending</span>
          </Show>
          <Show when={p.o.computed_at}>
            {(at) => <span class="w-16 text-right text-[11px] text-base-content/50" title={`${fmtTime(at())}, version ${p.o.rule_version}`}>{rel(at())}</span>}
          </Show>
        </span>
      </span>
      <Show when={p.o.no_members_since}>
        {(since) => (
          <span class="inline-flex items-center gap-1 text-[11px] text-warning">
            No members since {rel(since())}: the last value stands, unsupported
            <InfoTip label="no members" text={CALC_HINTS.noMembers} />
          </span>
        )}
      </Show>
      <Show when={!p.o.no_members_since && p.o.missing.length > 0}>
        <span class="text-[11px] text-warning">No value of <span class="font-data">{p.o.missing.join(", ")}</span>: nothing written</span>
      </Show>
      <Show when={p.o.error}>
        <span class="text-[11px] text-error">{p.o.error}</span>
      </Show>
      <For each={skippedByKey(p.o.skipped)}>
        {(g) => (
          <span class="text-[11px] text-base-content/55">
            No <span class="font-data">{g.key}</span> on:{" "}
            <For each={g.members.slice(0, SKIPPED_NAMES)}>
              {(m, i) => (
                <>
                  {i() > 0 ? ", " : ""}
                  <A href={`/components/${m.component_id}`} class="hover:underline">{m.component}</A>
                </>
              )}
            </For>
            <Show when={g.members.length > SKIPPED_NAMES}> and {g.members.length - SKIPPED_NAMES} more</Show>
          </span>
        )}
      </For>
      <Show when={p.o.skipped_hidden > 0}>
        <span class="text-[11px] text-base-content/55">Skipped: {hiddenWords(p.o.skipped_hidden, "member")}</span>
      </Show>
      <Show when={p.o.stale.length + p.o.stale_hidden > 0}>
        <span class="text-[11px] text-base-content/55">
          Stale, left out:{" "}
          {p.o.stale.map((s) => `${s.component} ${s.instance}`).join(", ")}
          <Show when={p.o.stale_hidden > 0}>{p.o.stale.length ? "; " : ""}{hiddenWords(p.o.stale_hidden, "instance")}</Show>
        </span>
      </Show>
    </li>
  );
}

function CalcBladeBody(p: { name: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const ui = useContext(CalcUiContext);
  const lookup = useCalcLookup(p.name);
  const row = lookup.row;
  const [err, setErr] = createSignal<string | null>(null);
  const ruleName = () => row()?.name ?? p.name;
  const [stateLimit, setStateLimit] = createSignal(STATE_ROWS);
  const state = useQuery(() => ({
    queryKey: calcStateKey(ruleName(), stateLimit()),
    queryFn: () => getCalcState(ruleName(), stateLimit()),
    refetchInterval: DETAIL_POLL_MS,
    enabled: !!row(),
    // Asking for more keeps the owners shown until the longer answer lands.
    placeholderData: (prev: Awaited<ReturnType<typeof getCalcState>> | undefined) => prev,
  }));
  const canUpdate = () => canFleet(me.data, "rule", "update");
  const canDelete = () => canFleet(me.data, "rule", "delete");

  async function toggleEnabled(r: CalcRule) {
    setErr(null);
    try {
      await updateCalcRule(r.name, { enabled: !r.enabled });
      await qc.invalidateQueries({ queryKey: CALC_RULES_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function remove(r: CalcRule) {
    if (!confirm(`Delete calc rule "${r.name}"? The values it wrote stay on their owners, as history naming it.`)) return;
    setErr(null);
    try {
      await deleteCalcRule(r.name);
      blades.close();
      await qc.invalidateQueries({ queryKey: CALC_RULES_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

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
      return canUpdate() ? null : registryLock(r, fleetView(me.data), "rule");
    },
  });

  return (
    <Show
      when={row()}
      fallback={
        <Show when={lookup.loaded()} fallback={<p class="text-sm text-base-content/40">Loading…</p>}>
          <p class="text-sm text-base-content/50">Calc rule not found.</p>
        </Show>
      }
    >
      {(r) => (
        <div class="flex flex-col gap-5">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <Show when={r().calc_error}>
            {(e) => (
              <div role="alert" data-testid="calc-error" class="alert alert-error alert-soft text-sm">
                <span>
                  The stored reduce does not compile ({e().field}{e().line ? `, line ${e().line}, column ${e().column}` : ""}):{" "}
                  <span class="font-data">{e().message}</span>. The rule computes nothing, and cannot be enabled, until it is rewritten.
                </span>
              </div>
            )}
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <KVStacked bind="name" value={<span class="font-data">{r().name}</span>} />
            <KVStacked label="Origin" value={<span class="inline-flex items-center gap-1.5">{originBadge(r().official)}<Show when={r().official}><InfoTip label="official rules" text={CALC_HINTS.official} /></Show></span>} />
            <KVStacked label="Derives on" value={<span class="inline-flex items-center gap-1.5">{ownerBadge(r().owner_kind)}<InfoTip label="owner kind" text={CALC_HINTS.owner} /></span>} />
            <KVStacked
              label="Status"
              value={
                <span class="inline-flex items-center gap-1.5">
                  {r().enabled ? <span class="badge badge-soft badge-success badge-sm">enabled</span> : <span class="badge badge-ghost badge-sm">disabled</span>}
                  <InfoTip label="enabled" text={CALC_HINTS.enabled} />
                </span>
              }
            />
            <KVStacked label="Version" value={<span class="tabular-nums">{r().version}</span>} />
            <KVStacked label="Writes" value={<span class="inline-flex items-center gap-1.5"><span class="font-data">{r().output_key}</span><span class="text-[11px] text-base-content/40">{r().output_lane}</span><InfoTip label="output" text={CALC_HINTS.output} /></span>} />
          </div>
          <Show when={r().description}>
            <KVStacked label="Description" value={<span class="text-base-content/80">{r().description}</span>} />
          </Show>

          <BindingLine outputKey={r().output_key} ownerKind={r().owner_kind} />

          <section class="flex flex-col gap-2">
            <Eyebrow label="Reads" hint={CALC_HINTS.inputs} />
            <div class="grid grid-cols-[auto_1fr] items-baseline gap-x-3 gap-y-1">
              <For each={r().inputs}>
                {(i) => (
                  <>
                    <code class="font-data text-[12px] text-base-content/60">{i.alias}</code>
                    <span class="font-data text-[12px]">{i.key}</span>
                  </>
                )}
              </For>
            </div>
            <Eyebrow label="Derives on" hint={CALC_HINTS.selector} class="mt-1" />
            <SelectorFacts rule={r()} />
          </section>

          <section class="flex flex-col gap-2">
            <Eyebrow label="Reduce" hint={CALC_HINTS.reduce} />
            <code data-testid="calc-expr" class="break-all font-data text-[12px] text-base-content/80 [font-variant-ligatures:none]">{r().reduce.expr}</code>
          </section>

          <section class="flex flex-col gap-2" aria-label="State">
            <Eyebrow
              label={`State${state.data ? ` (${state.data.total} owners, ${state.data.pending_depth} pending)` : ""}`}
              hint={CALC_HINTS.state}
            />
            <Show when={state.data} fallback={<span class="text-[12px] text-base-content/40">{state.isError ? describeError(state.error) : "Loading…"}</span>}>
              <Show when={state.data!.owners.length > 0} fallback={<span class="text-[12px] text-base-content/40">Nothing computed yet on an owner you can read.</span>}>
                {/* In the server's order: failing, pending, missing an input,
                    valued, each by name. */}
                <ul class="flex flex-col divide-y divide-base-300 rounded-box border border-base-300">
                  <For each={state.data!.owners}>{(o) => <StateRow o={o} />}</For>
                </ul>
                <Show when={state.data!.total > state.data!.owners.length}>
                  <Button
                    intent="quiet"
                    size="xs"
                    class="w-fit"
                    loading={state.isFetching && stateLimit() === STATE_ALL}
                    disabled={stateLimit() === STATE_ALL}
                    onClick={() => setStateLimit(STATE_ALL)}
                  >
                    and {state.data!.total - state.data!.owners.length} more
                  </Button>
                </Show>
              </Show>
            </Show>
          </section>
        </div>
      )}
    </Show>
  );
}

// BindingLine states what a rule's output is bound to (#1019): the key it
// writes on each owner, and that nothing else may write it, the why a tip
// away.
function BindingLine(p: { outputKey: string; ownerKind: OwnerKind }): JSX.Element {
  return (
    <p data-testid="calc-binding" class="text-[12px] leading-relaxed text-base-content/70">
      Writes <code class="font-data text-base-content">{p.outputKey}</code> on each {p.ownerKind}. No other rule or collector may write it.{" "}
      <span class="inline-flex align-middle">
        <InfoTip label="one writer" text={CALC_HINTS.binding} />
      </span>
    </p>
  );
}

// OutputCreate registers a rule's output from its own drawer (#1019): a new
// metric (a name, a label, a unit; its values are numbers) or property (a
// name, a label, a data type), through the create route the Metrics and
// Properties pages use, its refusals shown at the fields they name.
function OutputCreate(p: { lane: "metric" | "property"; onCreated: (name: string) => void; onCancel: () => void }): JSX.Element {
  const qc = useQueryClient();
  const noun = p.lane;
  const [name, setName] = createSignal("");
  const [label, setLabel] = createSignal("");
  const [unit, setUnit] = createSignal("");
  const [dataType, setDataType] = createSignal<PropertyDataType>("string");
  const [busy, setBusy] = createSignal(false);
  const [fieldErr, setFieldErr] = createSignal<Record<string, string>>({});
  const [formErr, setFormErr] = createSignal<string | null>(null);
  const id = createUniqueId();

  async function create() {
    setBusy(true);
    setFieldErr({});
    setFormErr(null);
    try {
      const lbl = label().trim() || undefined;
      if (p.lane === "metric") {
        const u = unit().trim() || undefined;
        const row = await createMetricType({ name: name().trim(), data_type: "float", ...(lbl ? { label: lbl } : {}), ...(u ? { unit: u } : {}) });
        qc.setQueryData<MetricRow[]>([...METRICS_KEY], (rows) => [...(rows ?? []), row]);
        void qc.invalidateQueries({ queryKey: METRICS_KEY });
        p.onCreated(row.name);
      } else {
        const row = await createProperty({ name: name().trim(), data_type: dataType(), ...(lbl ? { label: lbl } : {}) });
        qc.setQueryData<PropertyRow[]>([...PROPERTIES_KEY], (rows) => [...(rows ?? []), row]);
        void qc.invalidateQueries({ queryKey: PROPERTIES_KEY });
        p.onCreated(row.name);
      }
    } catch (e) {
      // A refusal of one field shows at it; anything else beneath them all.
      const o = e as { errors?: { location?: string; message?: string }[] } | null;
      const at: Record<string, string> = {};
      for (const fe of o?.errors ?? []) {
        const f = (fe.location ?? "").replace(/^body\./, "");
        if (fe.message && ["name", "label", "unit", "data_type"].includes(f)) at[f] = at[f] ? `${at[f]}; ${fe.message}` : fe.message;
      }
      setFieldErr(at);
      if (Object.keys(at).length === 0) setFormErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  const err = (f: string) => fieldErr()[f];
  // Each field's caption is visible, and contained in its accessible name.
  const field = (f: string, labelText: string, input: JSX.Element) => (
    <div class="flex flex-col gap-0.5">
      <span class="text-[11px] text-base-content/60" aria-hidden="true">{labelText}</span>
      {input}
      <Show when={err(f)}>{(m) => <p id={`${id}-${f}`} class="text-[11px] text-error">{labelText}: {m()}</p>}</Show>
    </div>
  );
  return (
    <div class="flex flex-col gap-2 rounded-box border border-base-300 p-2.5" role="group" aria-label={`New ${noun}`}>
      <span class="text-[11px] text-base-content/60">
        A new {noun} for the rule to write{p.lane === "metric" ? ": its values are numbers." : "."}
      </span>
      {field("name", "Name", (
        <input
          aria-label={`New ${noun} name`}
          aria-invalid={err("name") ? "true" : undefined}
          aria-describedby={err("name") ? `${id}-name` : undefined}
          class="input input-bordered input-sm w-full font-data"
          classList={{ "input-error": !!err("name") }}
          placeholder={p.lane === "metric" ? "tcp-worst" : "in-use"}
          value={name()}
          onInput={(e) => setName(e.currentTarget.value)}
        />
      ))}
      {field("label", "Label", (
        <input
          aria-label={`New ${noun} label`}
          aria-invalid={err("label") ? "true" : undefined}
          class="input input-bordered input-sm w-full"
          placeholder={p.lane === "metric" ? "Slowest TCP connect" : "In use"}
          value={label()}
          onInput={(e) => setLabel(e.currentTarget.value)}
        />
      ))}
      <Show
        when={p.lane === "metric"}
        fallback={field("data_type", "Data type", (
          <select
            aria-label={`New ${noun} data type`}
            class="select select-bordered select-sm w-full"
            value={dataType()}
            onChange={(e) => setDataType(e.currentTarget.value as PropertyDataType)}
          >
            <For each={PROPERTY_DATA_TYPES}>{(t) => <option value={t}>{t}</option>}</For>
          </select>
        ))}
      >
        {field("unit", "Unit", (
          <input
            aria-label={`New ${noun} unit`}
            aria-invalid={err("unit") ? "true" : undefined}
            class="input input-bordered input-sm w-full font-data"
            placeholder="ms"
            value={unit()}
            onInput={(e) => setUnit(e.currentTarget.value)}
          />
        ))}
      </Show>
      <Show when={formErr()}>{(m) => <p role="alert" class="text-[11px] text-error">{m()}</p>}</Show>
      <span class="flex gap-1.5">
        <Button intent="action" size="xs" icon={Plus} loading={busy()} disabled={!name().trim()} onClick={() => void create()}>
          Create {noun}
        </Button>
        <Button intent="quiet" size="xs" onClick={() => p.onCancel()}>Cancel</Button>
      </span>
    </div>
  );
}

// ---- the form ----------------------------------------------------------------

// CalcRuleForm authors a calc rule, on create and edit alike, in a Drawer that
// draws its action rail. An edit may rename (#977's pattern: the PATCH first,
// the :rename last, so a refused rename leaves the rest saved); the owner kind
// is fixed at create. The form is the inputs, each a key and the alias its
// expression binds it by (filled from the key's name until edited), then the
// expression editor against the calc site (checked as it is typed, with the
// inputs and owner kind it binds; start-from templates; a name nothing binds
// offered as a new input). Every rule is judged in TypeScript
// (lib/calc_rules, the alias rules generated from the server's) and gates
// the submit, with the expression's own verdict.
export function CalcRuleForm(p: { mode: "create" | "edit"; rule?: CalcRule; onSaved: (r: CalcRule) => void }): JSX.Element {
  const qc = useQueryClient();
  const properties = useQuery(() => ({ queryKey: PROPERTIES_KEY, queryFn: listProperties }));
  const metrics = useQuery(() => ({ queryKey: METRICS_KEY, queryFn: listMetricTypes }));
  const componentTypes = useQuery(() => ({ queryKey: COMPONENT_TYPES_KEY, queryFn: listComponentTypes }));
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
  const systemTypes = useQuery(() => ({ queryKey: SYSTEM_TYPES_KEY, queryFn: listSystemTypes }));
  const systems = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems }));
  const [d, setD] = createStore<CalcDraft>(p.rule ? draftFromCalcRule(p.rule) : emptyCalcDraft());
  const [touched, setTouched] = createSignal(false);
  // Leaving the name says what is wrong with the name alone: revealing every
  // field's fault then would move the form under the pointer.
  const [nameTouched, setNameTouched] = createSignal(false);
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);
  const [exprOk, setExprOk] = createSignal<boolean | undefined>(undefined);
  // The output being registered inline (#1019): a new metric or property.
  const [creating, setCreating] = createSignal<"metric" | "property" | null>(null);

  const errors = createMemo(() => validateCalcDraft(d, p.rule?.name));
  const valid = () => !hasErrors(errors()) && exprOk() === true;
  const shown = (msg: string | undefined) => (touched() ? msg : undefined);
  const nameErr = () => (touched() || nameTouched() ? errors().name : undefined);
  // An input's fault shows once the form is touched, or as soon as its alias
  // is the author's own and its key is chosen (so a refused alias says why
  // while it is typed).
  const inputErr = (i: number) => {
    const m = errors().input?.[i];
    const x = d.inputs[i];
    return touched() || (x && x.aliasEdited && x.key) ? m : undefined;
  };
  const byLabel = (a: { label?: string; name: string }, b: { label?: string; name: string }) => entityLabel(a).localeCompare(entityLabel(b));
  const keyOptions = createMemo(() => ({
    properties: [...(properties.data ?? [])].filter((x) => x.name !== "health").sort(byLabel),
    metrics: [...(metrics.data ?? [])].sort(byLabel),
  }));

  useFormActions().bind({
    submitLabel: p.mode === "create" ? "Create rule" : "Save rule",
    submitIcon: p.mode === "create" ? Plus : Save,
    submit: () => void submit(),
    busy,
    disabled: () => !valid(),
  });

  async function submit() {
    setTouched(true);
    if (!valid()) return;
    setBusy(true);
    setFormErr(null);
    try {
      if (p.mode === "create") {
        const saved = await createCalcRule(createCalcBody(d));
        await qc.invalidateQueries({ queryKey: CALC_RULES_KEY });
        p.onSaved(saved);
        return;
      }
      const was = p.rule!;
      const to = d.name.trim();
      const body = updateCalcBody(d, was);
      let saved = was;
      if (to === was.name || Object.keys(body).length > 0) saved = await updateCalcRule(was.name, body);
      if (to !== was.name) saved = await renameCalcRule(was.name, to);
      await qc.invalidateQueries({ queryKey: CALC_RULES_KEY });
      p.onSaved(saved);
    } catch (e) {
      setFormErr(describeError(e));
      if (p.mode === "edit") await qc.invalidateQueries({ queryKey: CALC_RULES_KEY });
    } finally {
      setBusy(false);
    }
  }

  // bindName gives a name the expression uses a new input of that alias,
  // and puts the focus on its key, the one thing left to choose.
  function bindName(name: string) {
    const n = d.inputs.length + 1;
    setD("inputs", [...d.inputs, { alias: name, key: "", aliasEdited: true }]);
    setTimeout(() => (document.querySelector(`[aria-label="Input ${n} key"]`) as HTMLElement | null)?.focus(), 0);
  }

  const KeySelect = (q: { value: string; label: string; onChange: (v: string) => void; invalid?: boolean }) => (
    <select
      aria-label={q.label}
      ref={bindSelectValue(() => q.value, () => keyOptions())}
      class="select select-bordered w-full"
      classList={{ "select-error": !!q.invalid }}
      onChange={(e) => q.onChange(e.currentTarget.value)}
    >
      <option value="" disabled>Choose a property or metric…</option>
      <optgroup label="Metrics">
        <For each={keyOptions().metrics}>{(m) => <option value={m.name}>{entityLabel(m)} ({m.name})</option>}</For>
      </optgroup>
      <optgroup label="Properties">
        <For each={keyOptions().properties}>{(x) => <option value={x.name}>{entityLabel(x)} ({x.name})</option>}</For>
      </optgroup>
    </select>
  );

  const sys = () => d.ownerKind === "system";
  const typeChoices = () => (sys() ? d.systemTypes : d.componentTypes);
  const idChoices = () => (sys() ? d.systems : d.components);
  const ownerOptions = createMemo(() =>
    (sys() ? (systems.data ?? []) : (components.data ?? [])).filter((o) => !idChoices().includes(o.id)).sort(byLabel),
  );
  const ownerName = (id: string) => {
    const hit = (sys() ? (systems.data ?? []) : (components.data ?? [])).find((o: { id: string }) => o.id === id);
    return hit ? entityLabel(hit) : id;
  };

  return (
    <form class="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <Show when={formErr()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{formErr()}</span></div>
      </Show>
      <FieldRow bind="name" info={CALC_HINTS.name}>
        <input
          class="input input-bordered w-full font-data"
          classList={{ "input-error": !!nameErr() }}
          aria-required="true"
          value={d.name}
          placeholder="busy-flag"
          onInput={(e) => setD("name", e.currentTarget.value)}
          onBlur={() => setNameTouched(true)}
        />
        <Show when={nameErr()}>{(m) => <p class="text-[11px] text-error">{m()}</p>}</Show>
      </FieldRow>
      <FieldRow bind="label">
        <input class="input input-bordered w-full" value={d.label} placeholder="Busy" onInput={(e) => setD("label", e.currentTarget.value)} />
      </FieldRow>

      <FieldRow label="Derives on" info={CALC_HINTS.owner}>
        <select
          ref={bindSelectValue(() => d.ownerKind, () => true)}
          class="select select-bordered w-full"
          disabled={p.mode === "edit"}
          onChange={(e) => setD({ ownerKind: e.currentTarget.value as OwnerKind, componentTypes: [], components: [], systemTypes: [], systems: [] })}
        >
          <option value="component">Each component</option>
          <option value="system">Each system, from its members</option>
        </select>
      </FieldRow>

      <FieldRow label={sys() ? "Of system types" : "Of component types"} info={CALC_HINTS.selector}>
        <Show
          when={sys()}
          fallback={
            <ComponentTypeSelect
              types={componentTypes.data ?? []}
              value=""
              emptyLabel={d.componentTypes.length ? "Add another component type…" : "Add a component type…"}
              onChange={(v) => { if (v && !d.componentTypes.includes(v)) setD("componentTypes", [...d.componentTypes, v]); }}
            />
          }
        >
          <select
            ref={bindSelectValue(() => "", () => systemTypes.data)}
            class="select select-bordered w-full"
            onChange={(e) => { const v = e.currentTarget.value; if (v && !d.systemTypes.includes(v)) setD("systemTypes", [...d.systemTypes, v]); e.currentTarget.value = ""; }}
          >
            <option value="">{d.systemTypes.length ? "Add another system type…" : "Add a system type…"}</option>
            <For each={[...(systemTypes.data ?? [])].sort(byLabel)}>{(t) => <option value={t.name}>{entityLabel(t)}</option>}</For>
          </select>
        </Show>
      </FieldRow>
      <FieldRow label={sys() ? "Or systems" : "Or components"}>
        <select
          ref={bindSelectValue(() => "", ownerOptions)}
          class="select select-bordered w-full"
          onChange={(e) => {
            const v = e.currentTarget.value;
            if (v) setD(sys() ? "systems" : "components", [...idChoices(), v]);
            e.currentTarget.value = "";
          }}
        >
          <option value="">{sys() ? "Add a system…" : "Add a component…"}</option>
          <For each={ownerOptions()}>{(o) => <option value={o.id}>{entityLabel(o)}</option>}</For>
        </select>
      </FieldRow>
      <div class="-mt-2 flex flex-wrap items-center gap-1.5" aria-label="Selected">
        <Show when={typeChoices().length + idChoices().length > 0} fallback={<span class="text-[12px] text-base-content/50">{sys() ? "Every system" : "Every component"}</span>}>
          <For each={typeChoices()}>
            {(t) => (
              <span class="badge badge-ghost badge-sm gap-1 font-data">
                {t}
                <button type="button" class="opacity-60 hover:opacity-100" aria-label={`Stop deriving on ${t}`} onClick={() => setD(sys() ? "systemTypes" : "componentTypes", typeChoices().filter((x) => x !== t))}>
                  <X size={11} />
                </button>
              </span>
            )}
          </For>
          <For each={idChoices()}>
            {(id) => (
              <span class="badge badge-outline badge-sm gap-1">
                {ownerName(id)}
                <button type="button" class="opacity-60 hover:opacity-100" aria-label={`Stop deriving on ${ownerName(id)}`} onClick={() => setD(sys() ? "systems" : "components", idChoices().filter((x) => x !== id))}>
                  <X size={11} />
                </button>
              </span>
            )}
          </For>
        </Show>
      </div>

      <section class="flex flex-col gap-2" aria-label="Inputs">
        <Eyebrow label="Reads" hint={CALC_HINTS.inputs} />
        <div class="grid grid-cols-[minmax(0,1fr)_minmax(0,9.5rem)_auto] items-center gap-x-2 gap-y-1">
          <span class="text-[11px] text-base-content/50">Key</span>
          <span class="text-[11px] text-base-content/50">Alias in the expression</span>
          <span />
          <Index each={d.inputs}>
            {(x, i) => {
              const errId = `calc-input-${i}-err`;
              return (
                <>
                  <KeySelect label={`Input ${i + 1} key`} value={x().key} onChange={(v) => setD("inputs", withInputKey(d.inputs, i, v))} invalid={!!inputErr(i) && !x().key} />
                  <input
                    aria-label={`Input ${i + 1} alias`}
                    aria-invalid={inputErr(i) ? "true" : undefined}
                    aria-describedby={inputErr(i) ? errId : undefined}
                    class="input input-bordered w-full font-data text-[13px] [font-variant-ligatures:none]"
                    classList={{ "input-error": !!inputErr(i) && !!x().key }}
                    placeholder="t"
                    spellcheck={false}
                    autocomplete="off"
                    value={x().alias}
                    onInput={(e) => setD("inputs", i, { alias: e.currentTarget.value, aliasEdited: true })}
                  />
                  <span class="flex w-6 justify-center">
                    <Show when={d.inputs.length > 1}>
                      <Button intent="quiet" size="xs" square icon={X} label={`Remove input ${i + 1}`} onClick={() => setD("inputs", d.inputs.filter((_, j) => j !== i))} />
                    </Show>
                  </span>
                  <Show when={inputErr(i)}>
                    {(m) => <p id={errId} class="col-span-3 -mt-0.5 text-[11px] text-error">{m()}</p>}
                  </Show>
                </>
              );
            }}
          </Index>
        </div>
        <Button intent="quiet" size="xs" class="w-fit" icon={Plus} onClick={() => setD("inputs", [...d.inputs, { alias: "", key: "", aliasEdited: false }])}>
          Add an input
        </Button>
      </section>

      <FieldRow label="Reduce" info={CALC_HINTS.reduce}>
        <ExpressionField
          site="calc"
          sampleKey={d.outputKey}
          inputs={boundInputs(d.inputs)}
          ownerKind={d.ownerKind}
          needsKey="Choose what the rule writes to check this expression."
          label="Reduce"
          placeholder={sys() ? "avg(t), or count(members, .has.t && .value.t > 30) * 1.0" : "max(t), or any(call) || max(people) > 0"}
          value={d.expr}
          onInput={(v) => setD("expr", v)}
          onCheck={(ok) => setExprOk(ok)}
          templates={calcTemplates(d.ownerKind)}
          onBind={bindName}
        />
        <Show when={shown(errors().reduce)}>{(m) => <p class="text-[11px] text-error">{m()}</p>}</Show>
      </FieldRow>

      <FieldRow label="Writes" info={CALC_HINTS.output}>
        <KeySelect label="Writes" value={d.outputKey} onChange={(v) => setD("outputKey", v)} invalid={!!shown(errors().outputKey)} />
        <Show when={shown(errors().outputKey)}>{(m) => <p class="text-[11px] text-error">{m()}</p>}</Show>
        <Show
          when={creating()}
          fallback={
            <span class="flex flex-wrap gap-1.5">
              <Button intent="quiet" size="xs" icon={Plus} onClick={() => setCreating("metric")}>New metric</Button>
              <Button intent="quiet" size="xs" icon={Plus} onClick={() => setCreating("property")}>New property</Button>
            </span>
          }
        >
          {(lane) => (
            <OutputCreate
              lane={lane()}
              onCancel={() => setCreating(null)}
              onCreated={(name) => {
                setCreating(null);
                setD("outputKey", name);
              }}
            />
          )}
        </Show>
        <Show when={d.outputKey}>{(k) => <BindingLine outputKey={k()} ownerKind={d.ownerKind} />}</Show>
      </FieldRow>
      <FieldRow label="Description">
        <input class="input input-bordered w-full" value={d.description} onInput={(e) => setD("description", e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Enabled" info={CALC_HINTS.enabled}>
        <input type="checkbox" class="toggle toggle-sm" checked={d.enabled} onChange={(e) => setD("enabled", e.currentTarget.checked)} />
      </FieldRow>

      <CalcPreviewPanel draft={d} editing={p.rule?.name} ready={valid()} />
    </form>
  );
}

// CalcPreviewPanel computes the draft now (POST /calc-rules:preview), on the
// owners it selects that the caller can read: the value it would write on
// each, whether that differs from what the owner holds, and what it found no
// value of, in the server's order. It writes nothing. A draft the server
// would refuse to save is refused here with the same reason.
function CalcPreviewPanel(p: { draft: CalcDraft; editing?: string; ready: boolean }): JSX.Element {
  const [result, setResult] = createSignal<CalcPreview | null>(null);
  const [busy, setBusy] = createSignal(false);
  const [failed, setFailed] = createSignal<string | null>(null);
  let section: HTMLElement | undefined;
  createEffect(on(() => JSON.stringify(previewCalcBody(p.draft, p.editing)), () => setResult(null), { defer: true }));
  async function run() {
    setBusy(true);
    setFailed(null);
    try {
      setResult(await previewCalcRule(previewCalcBody(p.draft, p.editing)));
      // The answer lands at the foot of a long drawer: bring it up to be read.
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
        hint={CALC_HINTS.preview}
        right={
          <Button intent="quiet" size="xs" loading={busy()} disabled={!p.ready} onClick={() => void run()}>
            Preview
          </Button>
        }
      />
      <Show when={failed()}>{(m) => <div role="alert" class="alert alert-error alert-soft text-sm"><span>{m()}</span></div>}</Show>
      <Show when={result()}>
        {(r) => (
          <div data-testid="calc-preview-result" class="flex flex-col gap-2 text-sm">
            <Show when={r().truncated}>
              <div data-testid="calc-preview-truncated" class="alert alert-warning alert-soft text-[12px]">
                <span>Computed {r().owners_judged} of {r().owners_total} owners: a preview stops at {r().owner_cap}.</span>
              </div>
            </Show>
            <Show when={r().owners.length > 0} fallback={<span class="text-[12px] text-base-content/50">It selects no owner you can read.</span>}>
              <ul class="flex max-h-60 flex-col gap-1 overflow-y-auto">
                {/* In the server's order: owners holding a value or that the
                    rule would write one on first, then by name. */}
                <For each={r().owners}>
                  {(o) => (
                    <li class="flex flex-col gap-0.5 text-[12px]">
                      <span class="flex items-center gap-2">
                        <A href={`/${o.owner_kind === "system" ? "systems" : "components"}/${o.owner_id}`} class="link link-hover">{o.owner}</A>
                        <Show
                          when={o.value !== undefined && o.value !== null}
                          fallback={<span class="text-warning">{o.no_members ? "no members: writes nothing" : o.error ? o.error : `no value of ${o.missing.join(", ")}`}</span>}
                        >
                          <span class="font-data">{stateValue(o.value)}</span>
                          <span class={`badge badge-sm ${o.differs ? "badge-soft badge-info" : "badge-ghost"}`}>{o.differs ? "would write" : "unchanged"}</span>
                        </Show>
                        <Show when={o.current !== undefined && o.current !== null && o.differs}>
                          <span class="ml-auto text-[11px] text-base-content/50">now <span class="font-data">{stateValue(o.current)}</span></span>
                        </Show>
                      </span>
                      <Show when={o.skipped.length + o.skipped_hidden > 0}>
                        <span class="text-[11px] text-base-content/50">
                          skipped {o.skipped.map((m) => m.component).join(", ")}
                          <Show when={o.skipped_hidden > 0}>{o.skipped.length ? "; " : ""}{hiddenWords(o.skipped_hidden, "member")}</Show>
                        </span>
                      </Show>
                    </li>
                  )}
                </For>
              </ul>
            </Show>
          </div>
        )}
      </Show>
    </section>
  );
}
