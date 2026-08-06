import { For, Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import BladeTitle from "../components/BladeTitle";
import FieldRow from "../components/FieldRow";
import BladeField from "../components/BladeField";
import { identityColumn } from "../components/IdentityCell";
import KVStacked from "../components/KVStacked";
import { useFormActions } from "../lib/formactions";
import { Plus } from "../components/icons";
import {
  type CommandTypeRow,
  COMMAND_TYPES_KEY,
  listCommandTypes,
  createCommandType,
  updateCommandType,
  deleteCommandType,
} from "../lib/command_types";
import { PROPERTIES_KEY, listProperties, type PropertyRow } from "../lib/properties";
import { METRICS_KEY, listMetricTypes, type MetricRow } from "../lib/metric_types";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Command Types: the "do" catalog (Catalog > Command Types), the twin of the
// Properties and Event Types catalogs. A command type names what a component can be
// told (set-input, reboot); a settleable one targets a property or a metric (the
// two-armed exclusive arc) and carries a settle window, a fire-and-forget one
// neither. Official (seed-owned) types are read-only.

function originBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

// The target is one fact with two forms, so one cell shows whichever arm is set
// with the lane named: a property target and a metric target settle against
// different stores and must be distinguishable at a glance.
type TargetArms = { target_property_type?: string; target_metric_type?: string };

function targetCell(arms: TargetArms): JSX.Element {
  const name = arms.target_property_type || arms.target_metric_type;
  if (!name) return <span class="text-base-content/30">fire-and-forget</span>;
  return (
    <span class="inline-flex items-baseline gap-1.5">
      <span class="font-data text-[12px]">{name}</span>
      <span class="text-[10px] text-base-content/40">{arms.target_property_type ? "property" : "metric"}</span>
    </span>
  );
}

// joinTarget folds the two wire arms into the picker's lane-qualified value
// ("property:video-input"); the lane prefix keeps the two catalogs apart even
// when a property and a metric share a name.
function joinTarget(arms: TargetArms | undefined): string {
  if (arms?.target_property_type) return `property:${arms.target_property_type}`;
  if (arms?.target_metric_type) return `metric:${arms.target_metric_type}`;
  return "";
}

// splitTarget unpacks a picker value into both wire arms. Sending both keeps a
// PATCH unambiguous: the chosen arm is set and the other explicitly cleared.
function splitTarget(v: string): { target_property_type: string; target_metric_type: string } {
  const sep = v.indexOf(":");
  const lane = sep < 0 ? "" : v.slice(0, sep);
  const name = sep < 0 ? "" : v.slice(sep + 1);
  return {
    target_property_type: lane === "property" ? name : "",
    target_metric_type: lane === "metric" ? name : "",
  };
}

// TargetSelect: the target picker over BOTH classifier catalogs in one control,
// grouped by lane; one selection sets one arm, so the exclusive arc cannot be
// violated from this form. Follows Products' VendorSelect idiom.
function TargetSelect(p: { value: string; onChange: (v: string) => void }): JSX.Element {
  const properties = useQuery(() => ({ queryKey: PROPERTIES_KEY, queryFn: listProperties }));
  const metrics = useQuery(() => ({ queryKey: METRICS_KEY, queryFn: listMetricTypes }));
  const byLabel = (a: { display_name?: string; name: string }, b: { display_name?: string; name: string }) =>
    (a.display_name || a.name).localeCompare(b.display_name || b.name);
  const propertyOptions = createMemo(() => [...(properties.data ?? [])].sort(byLabel) as PropertyRow[]);
  const metricOptions = createMemo(() => [...(metrics.data ?? [])].sort(byLabel) as MetricRow[]);
  return (
    <select class="select select-bordered w-full" value={p.value} onChange={(e) => p.onChange(e.currentTarget.value)}>
      <option value="">None (fire-and-forget)</option>
      <optgroup label="Properties">
        <For each={propertyOptions()}>
          {(r) => <option value={`property:${r.name}`}>{r.display_name || r.name}</option>}
        </For>
      </optgroup>
      <optgroup label="Metrics">
        <For each={metricOptions()}>
          {(m) => <option value={`metric:${m.name}`}>{m.display_name || m.name}</option>}
        </For>
      </optgroup>
    </select>
  );
}

// A command type's `name` may be dot-segmented (set-input, video-input) where the
// rest of the estate is kebab. That is a validation difference, not a different
// concept, so the header is the one word every list uses.
const columns: FlatColumn<CommandTypeRow>[] = [
  identityColumn<CommandTypeRow>(),
  { key: "target", label: "Target", cell: (r) => targetCell(r) },
  { key: "settle", label: "Settle (s)", width: "90px", sortVal: (r) => r.settle_window_seconds, cell: (r) => <span class="tabular-nums text-[12px]">{r.settle_window_seconds}</span> },
  { key: "official", label: "Origin", width: "100px", sortVal: (r) => String(r.official), cell: (r) => originBadge(r.official) },
];

export default function CommandTypes(): JSX.Element {
  const me = useMe();
  const commandTypes = useQuery(() => ({ queryKey: COMMAND_TYPES_KEY, queryFn: listCommandTypes }));
  const rows = () => (commandTypes.data ?? []).slice().sort((a, b) => a.name.localeCompare(b.name));
  const canCreate = () => can(me.data, "command_type", "create");

  return (
    <div class="flex min-h-full flex-col gap-4">
      <FlatList<CommandTypeRow>
        config={{
          entity: { name: "command type", plural: "command types" },
          rows,
          loading: () => commandTypes.isPending,
          error: () => commandTypes.error,
          filterKeys: [
            { key: "name", type: "string", hint: "substring", get: (r) => `${r.name} ${r.display_name ?? ""}`, values: () => [] },
            { key: "official", type: "string", hint: "exact", get: (r) => (r.official ? "official" : "custom"), values: () => ["official", "custom"] },
          ],
          filterPlaceholder: "filter command types by name, display name…",
          columns,
          empty: "No command types.",
          rowId: (r) => r.name,
          blades: { registry: { command_type: commandTypeBlade }, rootKind: "command_type" },
          create: canCreate()
            ? { label: "New command type", can: canCreate, body: (ctx) => <CreateCommandTypeForm onCreated={ctx.select} /> }
            : undefined,
        }}
      />
    </div>
  );
}

export const commandTypeBlade: BladeDef = {
  Title: (p) => <CommandTypeBladeTitle name={p.id} />,
  Body: (p) => <CommandTypeBladeBody name={p.id} />,
};

// The blade heading is the display name, falling back to the name, so opening a row
// lands on the same words the row showed. It rendered the bare name before, so
// clicking "ICMP RTT (avg)" opened a panel headed icmp.rtt-avg.
function CommandTypeBladeTitle(p: { name: string }): JSX.Element {
  return <BladeTitle row={useCommandTypeRow(p.name)} fallback={p.name} />;
}

function useCommandTypeRow(name: string): () => CommandTypeRow | undefined {
  const commandTypes = useQuery(() => ({ queryKey: COMMAND_TYPES_KEY, queryFn: listCommandTypes }));
  return () => (commandTypes.data ?? []).find((r) => r.name === name);
}

function CommandTypeBladeBody(p: { name: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const row = useCommandTypeRow(p.name);
  const [err, setErr] = createSignal<string | null>(null);
  const [displayName, setDisplayName] = createSignal("");
  const [description, setDescription] = createSignal("");
  const [settle, setSettle] = createSignal("0");
  const [target, setTarget] = createSignal("");

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    const r = row();
    setDisplayName(r?.display_name ?? "");
    setDescription(r?.description ?? "");
    setSettle(String(r?.settle_window_seconds ?? 0));
    setTarget(joinTarget(r));
    setErr(null);
  }));

  async function removeCommandType() {
    const r = row();
    if (!r) return;
    if (!confirm(`Delete command type "${r.name}"?`)) return;
    setErr(null);
    try {
      await deleteCommandType(r.name);
      blades.close();
      await qc.invalidateQueries({ queryKey: COMMAND_TYPES_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const r = row();
    if (!r) return;
    setErr(null);
    try {
      // Both target arms ride every save: the chosen arm set, the other
      // explicitly cleared, so a PATCH can change or clear the target wholesale.
      await updateCommandType(r.name, {
        display_name: displayName(), description: description(),
        settle_window_seconds: Number(settle()) || 0,
        ...splitTarget(target()),
      });
      await qc.invalidateQueries({ queryKey: COMMAND_TYPES_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e;
    }
  }

  edit.bind({
    editable: () => !!row() && !row()!.official && can(me.data, "command_type", "update"),
    save,
    destructive: () =>
      row() && !row()!.official && can(me.data, "command_type", "delete")
        ? { label: "Delete", tone: "danger", onClick: removeCommandType }
        : undefined,
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Command type not found.</p>}>
      {(r) => (
        <div class="flex flex-col gap-4">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <KVStacked bind="name" value={<span class="font-data">{r().name}</span>} />
            <KVStacked label="Target" value={targetCell(r())} />
            <KVStacked label="Settle window" value={<span class="tabular-nums">{r().settle_window_seconds}s</span>} />
            <KVStacked label="Origin" value={originBadge(r().official)} />
          </div>
          <BladeField
            bind="display_name"
            value={() => r().display_name ?? ""}
            draft={displayName}
            onInput={setDisplayName}
          />
          <BladeField
            label="Description"
            multiline
            value={() => r().description ?? ""}
            draft={description}
            onInput={setDescription}
          />
          <Show when={edit.editing()}>
            <FieldRow label="Target" eyebrow>
              <TargetSelect value={target()} onChange={setTarget} />
            </FieldRow>
            <FieldRow label="Settle window (seconds)" eyebrow>
              <input class="input input-bordered w-full font-data" type="number" min="0" value={settle()} onInput={(e) => setSettle(e.currentTarget.value)} />
            </FieldRow>
          </Show>
          <Show when={r().official}>
            <div role="alert" class="alert alert-soft text-sm"><span>Seed-owned, read-only.</span></div>
          </Show>
        </div>
      )}
    </Show>
  );
}

// CreateCommandTypeForm: register a custom command type. Only the name is required;
// a target (a property or a metric, one arm) and a settle window make it settleable.
export function CreateCommandTypeForm(p: { onCreated: (r: CommandTypeRow) => void }): JSX.Element {
  const qc = useQueryClient();
  const [name, setName] = createSignal("");
  const [displayName, setDisplayName] = createSignal("");
  const [description, setDescription] = createSignal("");
  const [target, setTarget] = createSignal("");
  const [settle, setSettle] = createSignal("0");
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  useFormActions().bind({
    submitLabel: "Create command type",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => !name().trim(),
  });

  async function submit() {
    setBusy(true);
    setFormErr(null);
    try {
      const arms = splitTarget(target());
      const created = await createCommandType({
        name: name().trim(),
        display_name: displayName().trim() || undefined,
        description: description().trim() || undefined,
        target_property_type: arms.target_property_type || undefined,
        target_metric_type: arms.target_metric_type || undefined,
        settle_window_seconds: Number(settle()) || 0,
      });
      await qc.invalidateQueries({ queryKey: COMMAND_TYPES_KEY });
      p.onCreated(created);
    } catch (er) {
      setFormErr(describeError(er));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form class="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <Show when={formErr()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{formErr()}</span></div>
      </Show>
      <FieldRow bind="name" hint="A lowercase, dot-hierarchied name, e.g. set-input or reboot.">
        <input class="input input-bordered w-full font-data" value={name()} placeholder="set-input" onInput={(e) => setName(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow bind="display_name">
        <input class="input input-bordered w-full" value={displayName()} placeholder="Set input" onInput={(e) => setDisplayName(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Description">
        <input class="input input-bordered w-full" value={description()} onInput={(e) => setDescription(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Target" hint="The value this command sets, for settlement: a property or a metric, never both. None for a fire-and-forget command.">
        <TargetSelect value={target()} onChange={setTarget} />
      </FieldRow>
      <FieldRow label="Settle window (seconds)" hint="How long the device is given to actuate before a mismatch is a failed command.">
        <input class="input input-bordered w-full font-data" type="number" min="0" value={settle()} onInput={(e) => setSettle(e.currentTarget.value)} />
      </FieldRow>
    </form>
  );
}
