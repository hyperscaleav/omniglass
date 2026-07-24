import { Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import KVStacked from "../components/KVStacked";
import { useFormActions } from "../lib/formactions";
import { Plus } from "../components/icons";
import {
  type Driver,
  DRIVERS_KEY,
  listDrivers,
  createDriver,
  updateDriver,
  deleteDriver,
} from "../lib/drivers";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Drivers: the driver registry (the driver picker on the product form), on the
// flat FlatList surface. A driver is addressed by its id (a kebab id,
// create-only); official (seed-owned) rows are read-only, same as the Types
// catalog's official rows: no Edit pencil, no Delete.

function officialBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

const columns: FlatColumn<Driver>[] = [
  { key: "id", label: "Id", sortVal: (d) => d.id, cell: (d) => <span class="font-data font-semibold">{d.id}</span> },
  { key: "display_name", label: "Display name", sortVal: (d) => d.display_name, cell: (d) => <span>{d.display_name}</span> },
  { key: "version", label: "Version", width: "110px", sortVal: (d) => d.version ?? "", cell: (d) => <span class="font-data text-xs text-base-content/60">{d.version || "—"}</span> },
  { key: "official", label: "Origin", width: "100px", sortVal: (d) => String(d.official), cell: (d) => officialBadge(d.official) },
];

export default function Drivers() {
  const me = useMe();
  const drivers = useQuery(() => ({ queryKey: DRIVERS_KEY, queryFn: listDrivers }));

  const rows = createMemo(() =>
    [...(drivers.data ?? [])].sort((a, b) => a.display_name.localeCompare(b.display_name) || a.id.localeCompare(b.id)),
  );

  return (
    <FlatList<Driver>
      config={{
        entity: { name: "driver", plural: "drivers" },
        rows,
        loading: () => drivers.isPending,
        error: () => drivers.error,
        filterKeys: [
          { key: "name", type: "string", hint: "substring", get: (d) => `${d.id} ${d.display_name}`, values: () => [] },
          { key: "official", type: "string", hint: "exact", get: (d) => (d.official ? "official" : "custom"), values: () => ["official", "custom"] },
        ],
        filterPlaceholder: "filter drivers by id, name…",
        columns,
        empty: "No drivers yet.",
        // Address a row by id: the write paths key on id, and it is globally unique.
        rowId: (d) => d.id,
        blades: { registry: { driver: driverBlade }, rootKind: "driver" },
        create: can(me.data, "driver", "create")
          ? { label: "New driver", can: () => can(me.data, "driver", "create"), body: (ctx) => <CreateDriverForm onCreated={ctx.close} /> }
          : undefined,
      }}
    />
  );
}

// driverBlade renders an id on the shared blade stack. An official row is
// read-only (no pencil, no destructive action); a custom row carries Edit +
// Delete.
export const driverBlade: BladeDef = {
  Title: (p) => <DriverBladeTitle id={p.id} />,
  Body: (p) => <DriverBladeBody id={p.id} />,
};

function useDriverRow(id: string): () => Driver | undefined {
  const drivers = useQuery(() => ({ queryKey: DRIVERS_KEY, queryFn: listDrivers }));
  return () => (drivers.data ?? []).find((d) => d.id === id);
}

function DriverBladeTitle(p: { id: string }): JSX.Element {
  const row = useDriverRow(p.id);
  return <span class="font-data">{row()?.id ?? p.id}</span>;
}

function DriverBladeBody(p: { id: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const row = useDriverRow(p.id);
  const [err, setErr] = createSignal<string | null>(null);
  const [displayName, setDisplayName] = createSignal("");
  const [version, setVersion] = createSignal("");

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    const r = row();
    setDisplayName(r?.display_name ?? "");
    setVersion(r?.version ?? "");
    setErr(null);
  }));

  async function removeDriver() {
    const r = row();
    if (!r) return;
    if (!confirm(`Delete driver "${r.id}"?`)) return;
    setErr(null);
    try {
      await deleteDriver(r.id);
      blades.close();
      await qc.invalidateQueries({ queryKey: DRIVERS_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const r = row();
    if (!r) return;
    setErr(null);
    try {
      await updateDriver(r.id, {
        display_name: displayName(),
        version: version(),
      });
      await qc.invalidateQueries({ queryKey: DRIVERS_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  edit.bind({
    editable: () => !!row() && !row()!.official && can(me.data, "driver", "update"),
    save,
    destructive: () =>
      row() && !row()!.official && can(me.data, "driver", "delete")
        ? { label: "Delete", tone: "danger", onClick: removeDriver }
        : undefined,
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Driver not found.</p>}>
      {(r) => (
        <div class="flex flex-col gap-4">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <KVStacked label="Id" value={<span class="font-data">{r().id}</span>} />
            <KVStacked label="Origin" value={officialBadge(r().official)} />
          </div>
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Display name</span>
            <Show
              when={edit.editing()}
              fallback={<div class="input input-bordered flex items-center text-sm">{r().display_name}</div>}
            >
              <input class="input input-bordered w-full" value={displayName()} onInput={(e) => setDisplayName(e.currentTarget.value)} />
            </Show>
          </div>
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Version</span>
            <Show
              when={edit.editing()}
              fallback={<div class="input input-bordered flex items-center text-sm font-data">{r().version || "—"}</div>}
            >
              <input class="input input-bordered w-full font-data" placeholder="1.0.0" value={version()} onInput={(e) => setVersion(e.currentTarget.value)} />
            </Show>
          </div>
          <Show when={r().official}>
            <div role="alert" class="alert alert-soft text-sm"><span>Seed-owned, read-only.</span></div>
          </Show>
        </div>
      )}
    </Show>
  );
}

// CreateDriverForm: name the id (a kebab id, immutable after creation) and set
// the display name; version is optional.
export function CreateDriverForm(p: { onCreated: (id: string) => void }): JSX.Element {
  const qc = useQueryClient();
  const [id, setId] = createSignal("");
  const [displayName, setDisplayName] = createSignal("");
  const [version, setVersion] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  useFormActions().bind({
    submitLabel: "Create driver",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => !id().trim() || !displayName().trim(),
  });

  async function submit() {
    setBusy(true);
    setFormErr(null);
    try {
      await createDriver({
        name: id().trim(),
        display_name: displayName().trim(),
        version: version().trim() || undefined,
      });
      await qc.invalidateQueries({ queryKey: DRIVERS_KEY });
      p.onCreated(id().trim());
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
      <Field label="Id" hint="A kebab id, e.g. snmp-generic.">
        <input class="input input-bordered w-full font-data" value={id()} placeholder="snmp-generic" onInput={(e) => setId(e.currentTarget.value)} />
      </Field>
      <Field label="Display name">
        <input class="input input-bordered w-full" value={displayName()} placeholder="Generic SNMP" onInput={(e) => setDisplayName(e.currentTarget.value)} />
      </Field>
      <Field label="Version" hint="A version string, e.g. 1.0.0. Optional.">
        <input class="input input-bordered w-full font-data" value={version()} placeholder="1.0.0" onInput={(e) => setVersion(e.currentTarget.value)} />
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
