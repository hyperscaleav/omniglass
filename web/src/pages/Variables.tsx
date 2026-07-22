import { For, Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import TreeSelect from "../components/TreeSelect";
import KVStacked from "../components/KVStacked";
import FieldRow from "../components/FieldRow";
import { useFormActions } from "../lib/formactions";
import { Plus } from "../components/icons";
import { type TreeNode } from "../lib/treeselect";
import {
  type Variable,
  type OwnerKind,
  type ValueType,
  VALUE_TYPES,
  VARIABLES_KEY,
  listVariables,
  createVariable,
  updateVariable,
  deleteVariable,
  displayValue,
  parseInput,
} from "../lib/variables";
import { SYSTEMS_KEY, listSystems } from "../lib/systems";
import { LOCATIONS_KEY, listLocations } from "../lib/locations";
import { COMPONENTS_KEY, listComponents } from "../lib/components";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Variables: the macro directory on the FlatList surface. A variable is a typed,
// plaintext free value owned at one scope (global, or a location / system /
// component) and resolved down the cascade; this page is the admin directory
// (create, inspect, edit, delete). A variable reaches a component by being sourced
// into a field; this directory manages the cells themselves.

const OWNER_KINDS: OwnerKind[] = ["global", "location", "system", "component"];

function ownerLabel(v: Variable): string {
  if (v.owner_kind === "global") return "Global";
  const tier = v.owner_kind.charAt(0).toUpperCase() + v.owner_kind.slice(1);
  return v.owner_name ? `${tier}: ${v.owner_name}` : tier;
}

const columns: FlatColumn<Variable>[] = [
  { key: "name", label: "Name", sortVal: (v) => v.name, cell: (v) => <span class="font-data font-semibold">{v.name}</span> },
  { key: "type", label: "Type", width: "120px", sortVal: (v) => v.value_type, cell: (v) => <span class="badge badge-ghost badge-sm">{v.value_type}</span> },
  { key: "owner", label: "Scope", width: "220px", sortVal: (v) => v.owner_kind, cell: (v) => <span class="text-base-content/70">{ownerLabel(v)}</span> },
  { key: "value", label: "Value", cell: (v) => <span class="font-data text-xs text-base-content/60">{displayValue(v.value)}</span> },
];

export default function Variables() {
  const me = useMe();
  const variables = useQuery(() => ({ queryKey: VARIABLES_KEY, queryFn: listVariables }));

  const rows = createMemo(() => [...(variables.data ?? [])].sort((a, b) => a.name.localeCompare(b.name)));

  return (
    <FlatList<Variable>
      config={{
        entity: { name: "variable", plural: "variables" },
        rows,
        loading: () => variables.isPending,
        error: () => variables.error,
        filterKeys: [
          { key: "name", type: "string", hint: "substring", get: (v) => `${v.name} ${v.value_type}`, values: () => [] },
          { key: "scope", type: "string", hint: "exact", get: (v) => v.owner_kind, values: (rs) => [...new Set(rs.map((r) => r.owner_kind))].sort() },
          { key: "type", type: "string", hint: "exact", get: (v) => v.value_type, values: (rs) => [...new Set(rs.map((r) => r.value_type))].sort() },
        ],
        filterPlaceholder: "filter variables by name, type, scope…",
        columns,
        empty: "No variables yet.",
        rowId: (v) => v.id,
        blades: { registry: { variable: variableBlade }, rootKind: "variable" },
        create: can(me.data, "variable", "create")
          ? { label: "New variable", can: () => can(me.data, "variable", "create"), body: (ctx) => <CreateVariableForm onCreated={ctx.close} /> }
          : undefined,
      }}
    />
  );
}

// variableBlade renders a variable on the shared blade stack. The footer carries
// Edit (value edit) for variable:update and Delete for variable:delete.
export const variableBlade: BladeDef = {
  Title: (p) => <VariableBladeTitle id={p.id} />,
  Body: (p) => <VariableBladeBody id={p.id} />,
};

function useVariableById(id: string): () => Variable | undefined {
  const variables = useQuery(() => ({ queryKey: VARIABLES_KEY, queryFn: listVariables }));
  return () => (variables.data ?? []).find((v) => v.id === id);
}

function VariableBladeTitle(p: { id: string }): JSX.Element {
  const variable = useVariableById(p.id);
  return <span class="font-data">{variable()?.name ?? "variable"}</span>;
}

function VariableBladeBody(p: { id: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const variable = useVariableById(p.id);
  const [err, setErr] = createSignal<string | null>(null);
  const [input, setInput] = createSignal("");

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    setInput(displayValue(variable()?.value));
    setErr(null);
  }));

  async function removeVariable() {
    const v = variable();
    if (!v) return;
    if (!confirm(`Delete variable "${v.name}" (${ownerLabel(v)})?`)) return;
    setErr(null);
    try {
      await deleteVariable(v.id);
      blades.close();
      await qc.invalidateQueries({ queryKey: VARIABLES_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const v = variable();
    if (!v) return;
    setErr(null);
    let value: unknown;
    try {
      value = parseInput(v.value_type as ValueType, input());
    } catch (e) {
      setErr(describeError(e));
      throw e;
    }
    try {
      await updateVariable(v.id, value);
      await qc.invalidateQueries({ queryKey: VARIABLES_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  edit.bind({
    editable: () => !!variable() && can(me.data, "variable", "update"),
    save,
    destructive: () => (variable() && can(me.data, "variable", "delete") ? { label: "Delete", tone: "danger", onClick: removeVariable } : undefined),
  });

  return (
    <Show when={variable()} fallback={<p class="text-sm text-base-content/50">Variable not found.</p>}>
      {(v) => (
        <div class="flex flex-col gap-4">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <KVStacked label="Type" value={<span class="badge badge-ghost badge-sm">{v().value_type}</span>} />
            <KVStacked label="Scope" value={<span>{ownerLabel(v())}</span>} />
          </div>
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Value</span>
            <Show
              when={edit.editing()}
              fallback={<ValueDisplay valueType={v().value_type} value={v().value} />}
            >
              <ValueInput valueType={v().value_type as ValueType} value={input()} onInput={setInput} />
            </Show>
          </div>
        </div>
      )}
    </Show>
  );
}

// ValueDisplay renders a resolved value read-only: a code block for a json value,
// an inline monospace value otherwise.
export function ValueDisplay(p: { valueType: string; value: unknown }): JSX.Element {
  return (
    <Show
      when={p.valueType === "json"}
      fallback={<div class="input input-bordered flex items-center font-data text-sm">{displayValue(p.value)}</div>}
    >
      <pre class="overflow-x-auto rounded-box border border-base-300 bg-base-200 p-3 font-data text-xs">{JSON.stringify(p.value, null, 2)}</pre>
    </Show>
  );
}

// ValueInput is the type-aware editor: a checkbox toggle for bool, a textarea for
// json, a number input for int/float, a text input for string. Exported so the
// effective-fields panel reuses the same control. The optional `class` rides on
// the rendered control so a caller
// can enrol it as a daisyUI `join-item` (KVRow does this).
export function ValueInput(p: { valueType: ValueType; value: string; onInput: (v: string) => void; class?: string; placeholder?: string }): JSX.Element {
  return (
    <Show when={p.valueType !== "bool"} fallback={
      <label class={["flex items-center gap-2", p.class].filter(Boolean).join(" ")}>
        <input type="checkbox" class="toggle toggle-sm" checked={p.value === "true"} onChange={(e) => p.onInput(e.currentTarget.checked ? "true" : "false")} />
        <span class="font-data text-sm">{p.value === "true" ? "true" : "false"}</span>
      </label>
    }>
      <Show when={p.valueType === "json"} fallback={
        <input
          class={["input input-bordered w-full font-data", p.class].filter(Boolean).join(" ")}
          type={p.valueType === "int" || p.valueType === "float" ? "number" : "text"}
          value={p.value}
          placeholder={p.placeholder}
          onInput={(e) => p.onInput(e.currentTarget.value)}
        />
      }>
        <textarea class={["textarea textarea-bordered w-full font-data text-sm", p.class].filter(Boolean).join(" ")} rows={4} value={p.value} placeholder={p.placeholder} onInput={(e) => p.onInput(e.currentTarget.value)} />
      </Show>
    </Show>
  );
}

// CreateVariableForm: name the key, pick a value type and an owner scope, then
// enter the value. The value is parsed to its type client-side and validated
// again server-side.
function CreateVariableForm(p: { onCreated: () => void }): JSX.Element {
  const qc = useQueryClient();
  const systems = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));

  const [name, setName] = createSignal("");
  const [valueType, setValueType] = createSignal<ValueType>("string");
  const [ownerKind, setOwnerKind] = createSignal<OwnerKind>("global");
  const [owner, setOwner] = createSignal("");
  const [value, setValue] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  const ownerTree = createMemo<TreeNode[]>(() => {
    switch (ownerKind()) {
      case "location": return (locations.data ?? []).map((l) => ({ id: l.name, value: l.name, label: l.display_name || l.name, parentId: l.parent }));
      case "system": return (systems.data ?? []).map((s) => ({ id: s.name, value: s.name, label: s.display_name || s.name, parentId: s.parent }));
      case "component": return (components.data ?? []).map((c) => ({ id: c.name, value: c.name, label: c.display_name || c.name, parentId: c.parent }));
      default: return [];
    }
  });

  useFormActions().bind({
    submitLabel: "Create variable",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => !name().trim() || (ownerKind() !== "global" && !owner()),
  });

  async function submit() {
    setBusy(true);
    setFormErr(null);
    let parsed: unknown;
    try {
      parsed = parseInput(valueType(), value());
    } catch (er) {
      setFormErr(describeError(er));
      setBusy(false);
      return;
    }
    try {
      await createVariable({
        name: name().trim(),
        value_type: valueType(),
        owner_kind: ownerKind(),
        owner: ownerKind() === "global" ? undefined : owner() || undefined,
        value: parsed,
      });
      await qc.invalidateQueries({ queryKey: VARIABLES_KEY });
      p.onCreated();
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
      <FieldRow label="Name" hint="The cascade key; unique per owner.">
        <input class="input input-bordered w-full font-data" value={name()} placeholder="poll_interval" onInput={(e) => setName(e.currentTarget.value)} />
      </FieldRow>
      <div class="grid grid-cols-2 gap-3">
        <FieldRow label="Type">
          <select class="select select-bordered w-full" value={valueType()} onChange={(e) => { setValueType(e.currentTarget.value as ValueType); setValue(""); }}>
            <For each={VALUE_TYPES}>{(t) => <option value={t}>{t}</option>}</For>
          </select>
        </FieldRow>
        <FieldRow
          label="Scope"
          info="The estate scope this variable attaches to. It cascades down onto the components below it: global, or a location, system, or component."
          docHref="https://docs.omniglass.hyperscaleav.com/architecture/variables/"
        >
          <select class="select select-bordered w-full" value={ownerKind()} onChange={(e) => { setOwnerKind(e.currentTarget.value as OwnerKind); setOwner(""); }}>
            <For each={OWNER_KINDS}>{(k) => <option value={k}>{k.charAt(0).toUpperCase() + k.slice(1)}</option>}</For>
          </select>
        </FieldRow>
      </div>
      <Show when={ownerKind() !== "global"}>
        <FieldRow label={ownerKind().charAt(0).toUpperCase() + ownerKind().slice(1)}>
          <TreeSelect items={ownerTree()} value={owner()} onChange={setOwner} rootLabel="Choose…" />
        </FieldRow>
      </Show>
      <FieldRow label="Value" hint={valueType() === "json" ? "A JSON object, array, or scalar." : valueType() === "bool" ? "true or false." : undefined}>
        <ValueInput valueType={valueType()} value={value()} onInput={setValue} />
      </FieldRow>
    </form>
  );
}
