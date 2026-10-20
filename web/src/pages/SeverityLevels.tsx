import { For, Show, createContext, createMemo, createSignal, useContext, type JSX } from "solid-js";
import { createStore } from "solid-js/store";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import Drawer from "../components/Drawer";
import BladeTitle from "../components/BladeTitle";
import FieldRow from "../components/FieldRow";
import KVStacked from "../components/KVStacked";
import InfoTip from "../components/InfoTip";
import SeverityBadge from "../components/SeverityBadge";
import { identityColumn } from "../components/IdentityCell";
import { Pencil, Plus, Save, Trash } from "../components/icons";
import { entityLabel } from "../lib/entities";
import {
  type LevelDraft,
  type LevelErrors,
  type SeverityLevel,
  SEVERITY_COLORS,
  createSeverityLevel,
  deleteSeverityLevel,
  draftFromLevel,
  emptyLevelDraft,
  levelCreateBody,
  levelUpdateBody,
  listSeverityLevels,
  outageFloor,
  rankPlace,
  readRank,
  severityBadge,
  severityLevelsKey,
  updateSeverityLevel,
  validateLevelDraft,
} from "../lib/severity";
import { EVENT_RULES_KEY } from "../lib/event_rules";
import { LOCATION_HEALTH_PREFIX, SYSTEM_HEALTH_PREFIX, SYSTEM_VERDICTS_KEY } from "../lib/health";
import { COMPONENT_ALARMS_PREFIX } from "../lib/alarms";
import { FLEET_VIEW_KEY } from "../lib/fleet";
import { COMPONENTS_KEY } from "../lib/components";
import { useMe, can } from "../lib/auth";
import { useFormActions } from "../lib/formactions";
import { bindSelectValue } from "../lib/selectvalue";
import { describeError } from "../lib/format";
import { type BladeDef, createBladeController, useBlades, useBladeEdit } from "../lib/blades";
import { OFFICIAL_LOCK, registryLock } from "../lib/catalog";

// Severity levels (Catalog > Actions > Severity, #921): the ranked registry an
// event rule raises at (#917), beside the rules. Each row is a level's rank, its
// label in its own color (the chip every alarm at it wears), whether an active
// alarm at it takes its component out, and its origin, worst first, the order an
// operator triages in. An operator level is created, renamed, relabelled,
// recolored, re-ranked and deleted here in a Drawer (#930), gated
// severity_level:* like every registry (the server asks no all-scope grant);
// official levels are read-only. The form teaches the rank: where the typed
// rank sits among the levels, and whether its alarms take a component out.

export const SEVERITY_HINTS = {
  levels:
    "How bad an alarm is. Every alarm, hand-raised or raised by a rule, names one of these levels; the console colors and sorts by it.",
  rank:
    "Higher is worse. The shipped levels are spaced ten apart, so a level of your own slots between two without renumbering.",
  outage:
    "An active alarm at an outage level takes its component out (it stops filling its roles); at any other level it degrades the component and leaves it in place. The line is fixed at the official High level's rank: a level ranked at or above it is an outage level. Re-ranking a level across the line recomputes the health of every component with an open alarm at it.",
  name: "A lowercase kebab name, e.g. major. Alarms and rules name the level by it; renaming it carries every alarm and rule with it.",
  color: "The hue every alarm at this level wears, from the console's palette.",
} as const;

// What an active alarm at the level does to its component, in one word. Only
// the outage wears a hue: a warning-toned "degrades" beside every lesser level
// competed with the level chips themselves.
function effectChip(l: SeverityLevel): JSX.Element {
  return l.outage
    ? <span class="badge badge-soft badge-error badge-sm">outage</span>
    : <span class="text-[12px] text-base-content/60">degrades</span>;
}

function originBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

const columns: FlatColumn<SeverityLevel>[] = [
  identityColumn<SeverityLevel>(),
  { key: "rank", label: "Rank", width: "72px", sortVal: (l) => l.rank, cell: (l) => <span class="font-data tabular-nums">{l.rank}</span> },
  { key: "looks", label: "Looks like", width: "110px", cell: (l) => <SeverityBadge name={l.name} /> },
  { key: "outage", label: "Effect", width: "96px", sortVal: (l) => String(l.outage), cell: effectChip },
  { key: "official", label: "Origin", width: "84px", sortVal: (l) => String(l.official), cell: (l) => originBadge(l.official) },
];

// The page owns the edit Drawer: a blade's Edit closes the stack and opens it
// (a form never stacks over a blade), and saving or closing lands back on the
// level's blade (by its new name after a rename).
type LevelsUi = { openEdit: (name: string) => void };
const LevelsUiContext = createContext<LevelsUi>();

export default function SeverityLevels(): JSX.Element {
  const me = useMe();
  const levels = useQuery(() => ({ queryKey: severityLevelsKey, queryFn: listSeverityLevels }));
  // Worst first.
  const rows = () => [...(levels.data ?? [])].sort((a, b) => b.rank - a.rank);
  const canCreate = () => can(me.data, "severity_level", "create");
  const blades = createBladeController();
  const [editing, setEditing] = createSignal<string | null>(null);
  const editRow = () => (levels.data ?? []).find((l) => l.name === editing());
  const backToBlade = (name: string) => {
    setEditing(null);
    blades.push({ kind: "severity_level", id: name });
  };

  return (
    <LevelsUiContext.Provider value={{ openEdit: (name) => { blades.close(); setEditing(name); } }}>
      <FlatList<SeverityLevel>
        config={{
          entity: { name: "severity level", plural: "severity levels" },
          rows,
          loading: () => levels.isPending,
          error: () => levels.error,
          filterKeys: [
            { key: "name", type: "string", hint: "substring", get: (l) => `${entityLabel(l)} ${l.name}`, values: () => [] },
            { key: "effect", type: "string", hint: "exact", get: (l) => (l.outage ? "outage" : "degrades"), values: () => ["outage", "degrades"] },
            { key: "official", type: "string", hint: "exact", get: (l) => (l.official ? "official" : "custom"), values: () => ["official", "custom"] },
          ],
          filterPlaceholder: "filter severity levels by name…",
          columns,
          empty: "No severity levels.",
          rowId: (l) => l.name,
          blades: { registry: { severity_level: severityLevelBlade }, rootKind: "severity_level", controller: blades },
          railExtra: () => (
            <span class="mr-1 inline-flex items-center gap-1.5 text-[12px] text-base-content/50">
              What a level does
              <InfoTip label="severity levels" text={SEVERITY_HINTS.levels} />
            </span>
          ),
          create: canCreate()
            ? { label: "New severity level", can: canCreate, body: (ctx) => <LevelForm mode="create" onSaved={ctx.select} /> }
            : undefined,
        }}
      />
      <Show when={editing() ? editRow() : undefined}>
        {(l) => (
          <Drawer open={true} onClose={() => backToBlade(l().name)} title={<span>Edit {entityLabel(l())}</span>}>
            <LevelForm mode="edit" level={l()} onSaved={(saved) => backToBlade(saved.name)} />
          </Drawer>
        )}
      </Show>
    </LevelsUiContext.Provider>
  );
}

function useLevelRow(name: string): () => SeverityLevel | undefined {
  const levels = useQuery(() => ({ queryKey: severityLevelsKey, queryFn: listSeverityLevels }));
  return () => (levels.data ?? []).find((l) => l.name === name);
}

export const severityLevelBlade: BladeDef = {
  Title: (p) => <BladeTitle row={useLevelRow(p.id)} fallback={p.id} />,
  Body: (p) => <SeverityLevelBladeBody name={p.id} />,
};

function SeverityLevelBladeBody(p: { name: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const ui = useContext(LevelsUiContext);
  const row = useLevelRow(p.name);
  const [err, setErr] = createSignal<string | null>(null);
  const canUpdate = () => can(me.data, "severity_level", "update");
  const canDelete = () => can(me.data, "severity_level", "delete");

  async function remove(l: SeverityLevel) {
    if (!confirm(`Delete severity level "${l.name}"? The server refuses it while an alarm or a rule still names it.`)) return;
    setErr(null);
    try {
      await deleteSeverityLevel(l.name);
      blades.close();
      await qc.invalidateQueries({ queryKey: severityLevelsKey });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  // An official level is read-only for everyone: the Edit / Delete pair greys
  // with the official sentence. An operator level offers Edit (the Drawer) and a
  // kebab Delete, each on its own permission; a caller holding neither sees the
  // pair greyed with what it needs.
  useBladeEdit().bind({
    primary: () => {
      const l = row();
      return l && !l.official && canUpdate() && ui ? { label: "Edit", icon: <Pencil size={15} />, onClick: () => ui.openEdit(l.name) } : undefined;
    },
    secondary: () => {
      const l = row();
      if (!l || l.official || !canDelete()) return [];
      return [{ label: "Delete", icon: <Trash size={15} />, tone: "danger", onClick: () => void remove(l) }];
    },
    locked: () => {
      const l = row();
      if (!l) return null;
      if (l.official) return OFFICIAL_LOCK;
      return canUpdate() ? null : registryLock(l, me.data, "severity_level");
    },
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Severity level not found.</p>}>
      {(l) => (
        <div class="flex flex-col gap-4">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <KVStacked bind="name" value={<span class="font-data">{l().name}</span>} />
            <KVStacked label="Origin" value={originBadge(l().official)} />
            <KVStacked
              label="Rank"
              value={
                <span class="inline-flex items-center gap-1.5">
                  <span class="font-data tabular-nums">{l().rank}</span>
                  <InfoTip label="rank" text={SEVERITY_HINTS.rank} />
                </span>
              }
            />
            <KVStacked label="Looks like" value={<SeverityBadge name={l().name} />} />
            <KVStacked
              label="Effect"
              value={
                <span class="inline-flex items-center gap-1.5">
                  {effectChip(l())}
                  <InfoTip label="effect" text={SEVERITY_HINTS.outage} />
                </span>
              }
            />
            <KVStacked label="Color" value={<span class="font-data">{l().color}</span>} />
          </div>
        </div>
      )}
    </Show>
  );
}

// ---- the form ----------------------------------------------------------------

// The query prefixes a level write moves: the levels themselves, the rules
// naming them, every component's alarms, every system's and place's health,
// the fleet verdicts and the fleet view.
const LEVEL_DEPENDENTS: readonly (readonly string[])[] = [
  severityLevelsKey,
  EVENT_RULES_KEY,
  COMPONENT_ALARMS_PREFIX,
  SYSTEM_HEALTH_PREFIX,
  LOCATION_HEALTH_PREFIX,
  SYSTEM_VERDICTS_KEY,
  FLEET_VIEW_KEY,
  COMPONENTS_KEY,
];

// LevelForm authors a level, on create and on edit alike; a Drawer hosts it and
// draws its action rail. Every rule is judged in TypeScript (lib/severity), shown
// beside its field once touched, and gates the submit. Beside the rank it says,
// live, where the typed rank sits among the levels and what an alarm at it does.
function LevelForm(p: { mode: "create" | "edit"; level?: SeverityLevel; onSaved: (l: SeverityLevel) => void }): JSX.Element {
  const qc = useQueryClient();
  const levels = useQuery(() => ({ queryKey: severityLevelsKey, queryFn: listSeverityLevels }));
  const all = () => levels.data ?? [];
  const [d, setD] = createStore<LevelDraft>(p.level ? draftFromLevel(p.level) : emptyLevelDraft());
  const [touched, setTouched] = createStore<Record<string, boolean>>({});
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  const errors = createMemo<LevelErrors>(() => validateLevelDraft(d, all(), p.level?.name));
  const valid = () => Object.keys(errors()).length === 0;
  const shown = (k: keyof LevelErrors) => (touched[k] ? errors()[k] : undefined);
  const touch = (k: keyof LevelErrors) => setTouched(k, true);
  const rank = () => readRank(d.rank);
  const floor = () => outageFloor(all());

  useFormActions().bind({
    submitLabel: p.mode === "create" ? "Create level" : "Save level",
    submitIcon: p.mode === "create" ? Plus : Save,
    submit: () => void submit(),
    busy,
    disabled: () => !valid(),
  });

  async function submit() {
    if (!valid()) {
      for (const k of Object.keys(errors())) touch(k as keyof LevelErrors);
      return;
    }
    setBusy(true);
    setFormErr(null);
    try {
      const saved = p.mode === "create" ? await createSeverityLevel(levelCreateBody(d)) : await updateSeverityLevel(p.level!.name, levelUpdateBody(d, p.level!));
      // A rename moves the name every alarm chip and rule reads, and a re-rank
      // can move a component across the outage line (the server recomputes
      // its health): every view that caches either is stale now.
      await Promise.all(LEVEL_DEPENDENTS.map((queryKey) => qc.invalidateQueries({ queryKey })));
      p.onSaved(saved);
    } catch (e) {
      setFormErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  const err = (k: keyof LevelErrors) => (
    <Show when={shown(k)}>{(m) => <p class="text-[11px] text-error">{m()}</p>}</Show>
  );

  return (
    <form class="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <Show when={formErr()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{formErr()}</span></div>
      </Show>
      <FieldRow bind="name" info={SEVERITY_HINTS.name}>
        <input
          class="input input-bordered w-full font-data"
          classList={{ "input-error": !!shown("name") }}
          aria-required="true"
          value={d.name}
          placeholder="major"
          onInput={(e) => { setD("name", e.currentTarget.value); touch("name"); }}
        />
        {err("name")}
      </FieldRow>
      <FieldRow bind="label">
        <input class="input input-bordered w-full" value={d.label} placeholder="Major" onInput={(e) => setD("label", e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Color" info={SEVERITY_HINTS.color}>
        <select
          ref={bindSelectValue(() => d.color, () => true)}
          class="select select-bordered w-full"
          classList={{ "select-error": !!shown("color") }}
          aria-required="true"
          onChange={(e) => { setD("color", e.currentTarget.value as LevelDraft["color"]); touch("color"); }}
        >
          <option value="" disabled>Choose a color…</option>
          <For each={SEVERITY_COLORS}>{(c) => <option value={c}>{c}</option>}</For>
        </select>
        {err("color")}
      </FieldRow>
      <div class="-mt-2 flex items-center gap-2 text-[12px] text-base-content/60">
        Its alarms wear
        <span data-testid="level-preview" class={`badge badge-sm ${severityBadge(d.color || undefined)}`}>
          {d.label.trim() || d.name.trim() || "this level"}
        </span>
      </div>
      <FieldRow label="Rank" info={SEVERITY_HINTS.rank}>
        <input
          class="input input-bordered w-full font-data"
          classList={{ "input-error": !!shown("rank") }}
          aria-required="true"
          inputmode="numeric"
          placeholder="35"
          value={d.rank}
          onInput={(e) => { setD("rank", e.currentTarget.value); touch("rank"); }}
        />
        {err("rank")}
      </FieldRow>
      <Show when={rank()}>
        {(r) => (
          <div class="-mt-2 flex flex-col gap-1 text-[12px] text-base-content/60">
            <span data-testid="level-place">{rankPlace(r(), all(), p.level?.name)}</span>
            <Show when={floor()}>
              {(f) => (
                <span class="inline-flex items-center gap-1.5" data-testid="level-effect">
                  {r() >= f()
                    ? <span class="badge badge-soft badge-error badge-xs">outage</span>
                    : <span class="badge badge-ghost badge-xs">degrades</span>}
                  {r() >= f() ? `its alarms take their component out (rank ${f()}+)` : `its alarms only degrade a component`}
                  <InfoTip label="the outage line" text={SEVERITY_HINTS.outage} />
                </span>
              )}
            </Show>
          </div>
        )}
      </Show>
    </form>
  );
}
