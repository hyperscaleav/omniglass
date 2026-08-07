import { For, Show, createEffect, createSignal, on, type JSX } from "solid-js";
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
  type MetricRow,
  type MetricDataType,
  METRIC_DATA_TYPES,
  METRICS_KEY,
  listMetricTypes,
  createMetricType,
  updateMetricType,
  deleteMetricType,
} from "../lib/metric_types";
import { useMe, can } from "../lib/auth";
import { registryLock } from "../lib/catalog";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Metrics: the numeric lane of the signal catalog (Catalog > Metrics), the twin
// of the Properties page. A metric type is a canonical numeric series a sample
// measures, carrying the facts the property lane never holds: a unit and a
// precision. Official rows are read-only; custom rows are
// operator-created. The catalog is estate-wide reference data, not a scoped
// resource, and every affordance gates on metric_type, the resource the API
// stamps.

function typeBadge(dataType: string): JSX.Element {
  return <span class="badge badge-ghost badge-sm font-data">{dataType}</span>;
}

function originBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

const columns: FlatColumn<MetricRow>[] = [
  identityColumn<MetricRow>(),
  { key: "data_type", label: "Type", width: "90px", sortVal: (r) => r.data_type, cell: (r) => typeBadge(r.data_type) },
  { key: "unit", label: "Unit", width: "90px", sortVal: (r) => r.unit ?? "", cell: (r) => <span class="font-data">{r.unit ?? ""}</span> },
  { key: "official", label: "Origin", width: "100px", sortVal: (r) => String(r.official), cell: (r) => originBadge(r.official) },
];

export default function Metrics(): JSX.Element {
  const me = useMe();
  const metrics = useQuery(() => ({ queryKey: METRICS_KEY, queryFn: listMetricTypes }));
  const rows = () => (metrics.data ?? []).slice().sort((a, b) => a.name.localeCompare(b.name));
  const canCreate = () => can(me.data, "metric_type", "create");

  return (
    <div class="flex min-h-full flex-col gap-4">
      <FlatList<MetricRow>
        config={{
          entity: { name: "metric", plural: "metrics" },
          rows,
          loading: () => metrics.isPending,
          error: () => metrics.error,
          filterKeys: [
            { key: "name", type: "string", hint: "substring", get: (r) => `${r.name} ${r.display_name ?? ""}`, values: () => [] },
            { key: "type", type: "string", hint: "exact", get: (r) => r.data_type, values: () => METRIC_DATA_TYPES },
            { key: "unit", type: "string", hint: "exact", get: (r) => r.unit ?? "", values: (rs) => [...new Set(rs.map((r) => r.unit ?? "").filter(Boolean))].sort() },
            { key: "official", type: "string", hint: "exact", get: (r) => (r.official ? "official" : "custom"), values: () => ["official", "custom"] },
          ],
          filterPlaceholder: "filter metrics by name, display name…",
          columns,
          empty: "No metrics.",
          rowId: (r) => r.name,
          blades: { registry: { metric: metricBlade }, rootKind: "metric" },
          create: canCreate()
            ? { label: "New metric", can: canCreate, body: (ctx) => <CreateMetricForm onCreated={ctx.select} /> }
            : undefined,
        }}
      />
    </div>
  );
}

// metricBlade renders one metric type on the shared blade stack. The title is
// what the list shows, the display name, falling back to the name; official
// metric types are read-only (the Edit / Delete pair greys).
export const metricBlade: BladeDef = {
  Title: (p) => <MetricBladeTitle name={p.id} />,
  Body: (p) => <MetricBladeBody name={p.id} />,
};

function MetricBladeTitle(p: { name: string }): JSX.Element {
  return <BladeTitle row={useMetricRow(p.name)} fallback={p.name} />;
}

function useMetricRow(name: string): () => MetricRow | undefined {
  const metrics = useQuery(() => ({ queryKey: METRICS_KEY, queryFn: listMetricTypes }));
  return () => (metrics.data ?? []).find((r) => r.name === name);
}

// parsePrecision maps the precision draft to what the PATCH carries: blank means
// unchanged (undefined), digits mean that many decimal places, anything else is
// the caller's error to surface.
function parsePrecision(draft: string): { ok: true; value?: number } | { ok: false } {
  const t = draft.trim();
  if (t === "") return { ok: true, value: undefined };
  if (!/^\d+$/.test(t)) return { ok: false };
  return { ok: true, value: Number(t) };
}

function MetricBladeBody(p: { name: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const row = useMetricRow(p.name);
  const [err, setErr] = createSignal<string | null>(null);
  const [displayName, setDisplayName] = createSignal("");
  const [description, setDescription] = createSignal("");
  const [unit, setUnit] = createSignal("");
  const [precision, setPrecision] = createSignal("");

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    const r = row();
    setDisplayName(r?.display_name ?? "");
    setDescription(r?.description ?? "");
    setUnit(r?.unit ?? "");
    setPrecision(r?.precision != null ? String(r.precision) : "");
    setErr(null);
  }));

  async function removeMetric() {
    const r = row();
    if (!r) return;
    if (!confirm(`Delete metric "${r.name}"?`)) return;
    setErr(null);
    try {
      await deleteMetricType(r.name);
      blades.close();
      await qc.invalidateQueries({ queryKey: METRICS_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const r = row();
    if (!r) return;
    setErr(null);
    const prec = parsePrecision(precision());
    if (!prec.ok) {
      setErr("Precision is a whole number of decimal places.");
      throw new Error("invalid precision"); // keep the blade in edit mode
    }
    try {
      await updateMetricType(r.name, {
        display_name: displayName(),
        description: description(),
        unit: unit().trim() || undefined,
        precision: prec.value,
      });
      await qc.invalidateQueries({ queryKey: METRICS_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  edit.bind({
    editable: () => !!row() && !row()!.official && can(me.data, "metric_type", "update"),
    save,
    destructive: () =>
      row() && !row()!.official && can(me.data, "metric_type", "delete")
        ? { label: "Delete", tone: "danger", onClick: removeMetric }
        : undefined,
    locked: () => registryLock(row(), me.data, "metric_type"),
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Metric not found.</p>}>
      {(r) => (
        <div class="flex flex-col gap-4">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <KVStacked bind="name" value={<span class="font-data">{r().name}</span>} />
            <KVStacked label="Type" value={typeBadge(r().data_type)} />
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
          <BladeField
            label="Unit"
            mono
            placeholder="ms"
            hint="The display unit of the series (ms, dB, percent)."
            value={() => r().unit ?? ""}
            draft={unit}
            onInput={setUnit}
          />
          <BladeField
            label="Precision"
            placeholder="1"
            hint="Decimal places a rendered value keeps. Blank leaves it unchanged."
            value={() => (r().precision != null ? String(r().precision) : "")}
            draft={precision}
            onInput={setPrecision}
          />
        </div>
      )}
    </Show>
  );
}

// CreateMetricForm: register a custom metric type. Name and data type are
// required; the data types are the numeric lane (a categorical signal is a
// property).
export function CreateMetricForm(p: { onCreated: (r: MetricRow) => void }): JSX.Element {
  const qc = useQueryClient();
  const [name, setName] = createSignal("");
  const [dataType, setDataType] = createSignal<MetricDataType>("float");
  const [displayName, setDisplayName] = createSignal("");
  const [description, setDescription] = createSignal("");
  const [unit, setUnit] = createSignal("");
  const [precision, setPrecision] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  useFormActions().bind({
    submitLabel: "Create metric",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => !name().trim(),
  });

  async function submit() {
    const prec = parsePrecision(precision());
    if (!prec.ok) {
      setFormErr("Precision is a whole number of decimal places.");
      return;
    }
    setBusy(true);
    setFormErr(null);
    try {
      const created = await createMetricType({
        name: name().trim(),
        data_type: dataType(),
        display_name: displayName().trim() || undefined,
        description: description().trim() || undefined,
        unit: unit().trim() || undefined,
        precision: prec.value,
      });
      await qc.invalidateQueries({ queryKey: METRICS_KEY });
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
      <FieldRow bind="name" hint="A lowercase kebab name, e.g. icmp-rtt-avg or cpu-load.">
        <input class="input input-bordered w-full font-data" value={name()} placeholder="icmp-rtt-avg" onInput={(e) => setName(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Data type">
        <select class="select select-bordered w-full" value={dataType()} onChange={(e) => setDataType(e.currentTarget.value as MetricDataType)}>
          <For each={METRIC_DATA_TYPES}>{(t) => <option value={t}>{t}</option>}</For>
        </select>
      </FieldRow>
      <FieldRow bind="display_name">
        <input class="input input-bordered w-full" value={displayName()} placeholder="ICMP RTT (avg)" onInput={(e) => setDisplayName(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Description">
        <input class="input input-bordered w-full" value={description()} onInput={(e) => setDescription(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Unit" hint="The display unit of the series (ms, dB, percent).">
        <input class="input input-bordered w-full font-data" value={unit()} placeholder="ms" onInput={(e) => setUnit(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Precision" hint="Decimal places a rendered value keeps.">
        <input class="input input-bordered w-full" inputmode="numeric" value={precision()} placeholder="1" onInput={(e) => setPrecision(e.currentTarget.value)} />
      </FieldRow>
    </form>
  );
}
