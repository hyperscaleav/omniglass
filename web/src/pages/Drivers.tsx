import { Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
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
  type Driver,
  DRIVERS_KEY,
  listDrivers,
  createDriver,
  updateDriver,
  deleteDriver,
} from "../lib/drivers";
import { useMe, can } from "../lib/auth";
import { registryLock } from "../lib/catalog";
import { createIdentity, entityLabel } from "../lib/entities";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Drivers: the driver registry (the driver picker on the product form), on the
// flat FlatList surface. A driver is addressed by its kebab name (ADR-0062:
// the uuid is identity, the name is what an operator reads and types);
// official rows are read-only, same as the Types catalog's
// official rows: the Edit / Delete pair greys.

function officialBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

const columns: FlatColumn<Driver>[] = [
  identityColumn<Driver>(),
  { key: "version", label: "Version", width: "110px", sortVal: (d) => d.version ?? "", cell: (d) => <span class="font-data text-xs text-base-content/60">{d.version || "—"}</span> },
  { key: "official", label: "Origin", width: "100px", sortVal: (d) => String(d.official), cell: (d) => officialBadge(d.official) },
];

export default function Drivers() {
  const me = useMe();
  const drivers = useQuery(() => ({ queryKey: DRIVERS_KEY, queryFn: listDrivers }));

  const rows = createMemo(() =>
    [...(drivers.data ?? [])].sort((a, b) => a.label.localeCompare(b.label) || a.name.localeCompare(b.name)),
  );

  return (
    <FlatList<Driver>
      config={{
        entity: { name: "driver", plural: "drivers" },
        rows,
        loading: () => drivers.isPending,
        error: () => drivers.error,
        filterKeys: [
          { key: "name", type: "string", hint: "substring", get: (d) => `${entityLabel(d)} ${d.name}`, values: () => [] },
          { key: "official", type: "string", hint: "exact", get: (d) => (d.official ? "official" : "custom"), values: () => ["official", "custom"] },
        ],
        filterPlaceholder: "filter drivers by name…",
        columns,
        empty: "No drivers yet.",
        // Address a row by its kebab name: globally unique, and what the API
        // and CLI accept (the write paths resolve either form).
        rowId: (d) => d.name,
        blades: { registry: { driver: driverBlade }, rootKind: "driver" },
        create: can(me.data, "driver", "create")
          ? { label: "New driver", can: () => can(me.data, "driver", "create"), body: (ctx) => <CreateDriverForm onCreated={ctx.select} /> }
          : undefined,
      }}
    />
  );
}

// driverBlade renders an id on the shared blade stack. An official row is
// read-only (the Edit / Delete pair greys); a custom row carries Edit +
// Delete.
export const driverBlade: BladeDef = {
  Title: (p) => <DriverBladeTitle id={p.id} />,
  Body: (p) => <DriverBladeBody id={p.id} />,
};

function useDriverRow(id: string): () => Driver | undefined {
  const drivers = useQuery(() => ({ queryKey: DRIVERS_KEY, queryFn: listDrivers }));
  return () => (drivers.data ?? []).find((d) => d.name === id);
}

function DriverBladeTitle(p: { id: string }): JSX.Element {
  return <BladeTitle row={useDriverRow(p.id)} fallback={p.id} />;
}

function DriverBladeBody(p: { id: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const row = useDriverRow(p.id);
  const [err, setErr] = createSignal<string | null>(null);
  const [label, setLabel] = createSignal("");
  const [version, setVersion] = createSignal("");

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    const r = row();
    setLabel(r?.label ?? "");
    setVersion(r?.version ?? "");
    setErr(null);
  }));

  async function removeDriver() {
    const r = row();
    if (!r) return;
    if (!confirm(`Delete driver "${r.name}"?`)) return;
    setErr(null);
    try {
      await deleteDriver(r.name);
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
      await updateDriver(r.name, {
        label: label(),
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
    locked: () => registryLock(row(), me.data, "driver"),
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Driver not found.</p>}>
      {(r) => (
        <div class="flex flex-col gap-4">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <KVStacked bind="name" value={<span class="font-data">{r().name}</span>} />
            <KVStacked label="Origin" value={officialBadge(r().official)} />
            <KVStacked label="Id" value={<span class="font-data text-xs text-base-content/60">{r().id}</span>} />
          </div>
          <BladeField
            bind="label"
            value={() => r().label ?? ""}
            draft={label}
            onInput={setLabel}
          />
          <BladeField
            label="Version"
            mono
            placeholder="1.0.0"
            value={() => r().version ?? ""}
            draft={version}
            onInput={setVersion}
          />
        </div>
      )}
    </Show>
  );
}

// CreateDriverForm: type the label and the kebab name (the operator-facing
// address; the uuid is the database's to mint) follows it, until the operator
// takes the name over by hand. Version is optional.
export function CreateDriverForm(p: { onCreated: (d: Driver) => void }): JSX.Element {
  const qc = useQueryClient();
  const { display, setDisplay, name, setName, nameDerived } = createIdentity();
  const [version, setVersion] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  useFormActions().bind({
    submitLabel: "Create driver",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => !name().trim() || !display().trim(),
  });

  async function submit() {
    setBusy(true);
    setFormErr(null);
    try {
      const created = await createDriver({
        name: name().trim(),
        label: display().trim(),
        version: version().trim() || undefined,
      });
      await qc.invalidateQueries({ queryKey: DRIVERS_KEY });
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
      <FieldRow bind="label">
        <input class="input input-bordered w-full" value={display()} placeholder="Generic SNMP" onInput={(e) => setDisplay(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow bind="name" hint={nameDerived() ? "Derived from the label. Edit to set your own." : "A kebab name, e.g. snmp-generic."}>
        <input class="input input-bordered w-full font-data" value={name()} placeholder="snmp-generic" onInput={(e) => setName(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Version" hint="A version string, e.g. 1.0.0. Optional.">
        <input class="input input-bordered w-full font-data" value={version()} placeholder="1.0.0" onInput={(e) => setVersion(e.currentTarget.value)} />
      </FieldRow>
    </form>
  );
}
