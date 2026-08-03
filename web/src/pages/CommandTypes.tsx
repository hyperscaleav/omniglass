import { Show, createEffect, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
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
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Command Types: the "do" catalog (Catalog > Command Types), the twin of the
// Properties and Event Types catalogs. A command type names what a component can be
// told (set_input, reboot); a settleable one targets a property and carries a settle
// window, a fire-and-forget one neither. Official (seed-owned) types are read-only.

function originBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

function targetCell(target: string | undefined): JSX.Element {
  return target
    ? <span class="font-data text-[12px]">{target}</span>
    : <span class="text-base-content/30">fire-and-forget</span>;
}

// The header stays "Key" rather than the primitive's default "Name": a command
// type's `name` is a keyspace key (set_input, video.input), not the kebab segment
// the rest of the estate is addressed by, and calling it a name would be a lie.
const columns: FlatColumn<CommandTypeRow>[] = [
  identityColumn<CommandTypeRow>({ label: "Key" }),
  { key: "target", label: "Target", cell: (r) => targetCell(r.target_property_type) },
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
          filterPlaceholder: "filter command types by name, label…",
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
  Title: (p) => <span class="font-data">{p.id}</span>,
  Body: (p) => <CommandTypeBladeBody name={p.id} />,
};

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

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    const r = row();
    setDisplayName(r?.display_name ?? "");
    setDescription(r?.description ?? "");
    setSettle(String(r?.settle_window_seconds ?? 0));
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
      await updateCommandType(r.name, {
        display_name: displayName(), description: description(),
        settle_window_seconds: Number(settle()) || 0,
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
            <KVStacked label="Target property" value={targetCell(r().target_property_type)} />
            <KVStacked label="Settle window" value={<span class="tabular-nums">{r().settle_window_seconds}s</span>} />
            <KVStacked label="Origin" value={originBadge(r().official)} />
          </div>
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Display name</span>
            <Show when={edit.editing()} fallback={<div class="input input-bordered flex items-center text-sm">{r().display_name}</div>}>
              <input class="input input-bordered w-full" value={displayName()} onInput={(e) => setDisplayName(e.currentTarget.value)} />
            </Show>
          </div>
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Description</span>
            <Show when={edit.editing()} fallback={<div class="input input-bordered flex items-center text-sm">{r().description}</div>}>
              <input class="input input-bordered w-full" value={description()} onInput={(e) => setDescription(e.currentTarget.value)} />
            </Show>
          </div>
          <Show when={edit.editing()}>
            <div class="flex flex-col gap-1.5">
              <span class="eyebrow">Settle window (seconds)</span>
              <input class="input input-bordered w-full font-data" type="number" min="0" value={settle()} onInput={(e) => setSettle(e.currentTarget.value)} />
            </div>
          </Show>
          <Show when={r().official}>
            <div role="alert" class="alert alert-soft text-sm"><span>Seed-owned, read-only.</span></div>
          </Show>
        </div>
      )}
    </Show>
  );
}

// CreateCommandTypeForm: register a custom command type. Only the name is required; a
// target property and a settle window make it settleable.
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
      const created = await createCommandType({
        name: name().trim(),
        display_name: displayName().trim() || undefined,
        description: description().trim() || undefined,
        target_property_type: target().trim() || undefined,
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
      <Field label="Key" hint="A lowercase, dot-hierarchied name, e.g. set_input or reboot.">
        <input class="input input-bordered w-full font-data" value={name()} placeholder="set_input" onInput={(e) => setName(e.currentTarget.value)} />
      </Field>
      <Field label="Display name">
        <input class="input input-bordered w-full" value={displayName()} placeholder="Set input" onInput={(e) => setDisplayName(e.currentTarget.value)} />
      </Field>
      <Field label="Description">
        <input class="input input-bordered w-full" value={description()} onInput={(e) => setDescription(e.currentTarget.value)} />
      </Field>
      <Field label="Target property" hint="The property this command sets, for settlement. Leave blank for a fire-and-forget command.">
        <input class="input input-bordered w-full font-data" value={target()} placeholder="video.input" onInput={(e) => setTarget(e.currentTarget.value)} />
      </Field>
      <Field label="Settle window (seconds)" hint="How long the device is given to actuate before a mismatch is a failed command.">
        <input class="input input-bordered w-full font-data" type="number" min="0" value={settle()} onInput={(e) => setSettle(e.currentTarget.value)} />
      </Field>
    </form>
  );
}

function Field(p: { label: string; hint?: string; children: JSX.Element }): JSX.Element {
  return (
    <label class="flex flex-col gap-1">
      <span class="text-[12px] font-medium text-base-content/70">{p.label}</span>
      {p.children}
      <Show when={p.hint}><span class="text-[11px] text-base-content/40">{p.hint}</span></Show>
    </label>
  );
}
