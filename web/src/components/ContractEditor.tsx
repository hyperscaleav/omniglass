import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import { Check, Pencil, Plus, Trash, X } from "./icons";
import { PROPERTIES_KEY, listProperties } from "../lib/properties";
import { METRICS_KEY, listMetricTypes } from "../lib/metric_types";
import {
  CLASSIFIER_RESOURCE,
  classifierProperties,
  classifierPropertiesKey,
  deleteClassifierProperty,
  setClassifierProperty,
  type ClassifierKind,
  type SetClassifierProperty,
} from "../lib/classifier_properties";
import {
  classifierMetrics,
  classifierMetricsKey,
  deleteClassifierMetric,
  setClassifierMetric,
} from "../lib/classifier_metrics";
import { displayValue, parseInput, type ValueType } from "../lib/variables";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";

// ContractEditor is the classifier detail-blade panel for curating a declared
// contract: which catalog signals every owner of the classifier exposes, and
// what each one defaults to. One editor serves all three arcs, named by the
// classifier it edits:
//
//   product       -> a component of the product
//   standard      -> a system conforming to the standard
//   location-type -> a location of the type
//
// and both catalog lanes (#587), named by the lane prop: the property lane
// (the default), where an owner resolves a declared property to its own
// override or the default here, and the metric lane, where an owner resolves a
// declared metric to its series' latest sample or the default here (a metric's
// values are telemetry, so nothing is "set" on a detail page). The lane picks
// the routes, the catalog the picker offers, and the copy; the row mechanics
// are shared.
//
// Each line is addressed by name, so a write is a PUT (idempotent: an edit
// revises the line in place) and a withdraw is a DELETE. Writes are immediate, like
// the tag panel, so the panel has no Save of its own and does not contend with the
// blade's edit slot (which the classifier's core facts already own). Declaring needs
// the classifier's :update, withdrawing its :delete, and an official (seed-owned)
// classifier's contract is read-only: the list renders, the controls do not.

export type ContractLane = "property" | "metric";

// The per-kind language: what the classifier is called and what it declares for.
type ContractCopy = { hint: string; lede: string; empty: string; confirm: (name: string) => string };

const CONTRACT_COPY: Record<ClassifierKind, ContractCopy> = {
  product: {
    hint: "the product contract",
    lede: "A component of this product inherits every property declared here, resolved to the default below unless the component overrides it.",
    empty: "This product declares no properties.",
    confirm: (property) => `Withdraw "${property}" from this product's contract?`,
  },
  standard: {
    hint: "the standard contract",
    lede: "A system conforming to this standard inherits every property declared here, resolved to the default below unless the system overrides it.",
    empty: "This standard declares no properties.",
    confirm: (property) => `Withdraw "${property}" from this standard's contract?`,
  },
  "location-type": {
    hint: "the location type contract",
    lede: "A location of this type inherits every property declared here, resolved to the default below unless the location overrides it.",
    empty: "This location type declares no properties.",
    confirm: (property) => `Withdraw "${property}" from this location type's contract?`,
  },
};

const METRIC_CONTRACT_COPY: Record<ClassifierKind, ContractCopy> = {
  product: {
    hint: "the product contract",
    lede: "A component of this product carries every metric declared here, resolved to its series' latest sample, or to the default below until one arrives.",
    empty: "This product declares no metrics.",
    confirm: (metric) => `Withdraw "${metric}" from this product's contract?`,
  },
  standard: {
    hint: "the standard contract",
    lede: "A system conforming to this standard carries every metric declared here, resolved to its series' latest sample, or to the default below until one arrives.",
    empty: "This standard declares no metrics.",
    confirm: (metric) => `Withdraw "${metric}" from this standard's contract?`,
  },
  "location-type": {
    hint: "the location type contract",
    lede: "A location of this type carries every metric declared here, resolved to its series' latest sample, or to the default below until one arrives.",
    empty: "This location type declares no metrics.",
    confirm: (metric) => `Withdraw "${metric}" from this location type's contract?`,
  },
};

// contractLine is the lane-neutral shape of one wire line: the property lane
// names it property_type_name, the metric lane metric_type_name, so the lane
// carries an accessor rather than the cache re-keying the wire shape.
type contractLine = {
  property_type_name?: string;
  metric_type_name?: string;
  default_value?: unknown;
  required: boolean;
};

// catalogRow is the lane-neutral shape of one catalog entry; PropertyRow and
// MetricRow both satisfy it.
type catalogRow = { name: string; data_type: string; display_name?: string };

// laneConfig is everything that differs between the two lanes: the data layer,
// the catalog the picker offers, the copy, and the add-row labels.
type laneConfig = {
  heading: string;
  copy: Record<ClassifierKind, ContractCopy>;
  catalogKey: readonly unknown[];
  listCatalog: () => Promise<catalogRow[]>;
  key: (kind: ClassifierKind, id: string) => readonly unknown[];
  list: (kind: ClassifierKind, id: string) => Promise<contractLine[]>;
  set: (kind: ClassifierKind, id: string, name: string, body: SetClassifierProperty) => Promise<unknown>;
  del: (kind: ClassifierKind, id: string, name: string) => Promise<void>;
  nameOf: (line: contractLine) => string;
  pickerLabel: string;
  pickerPrompt: string;
  addDefaultLabel: string;
  declareLabel: string;
  exhausted: string;
};

const LANES: Record<ContractLane, laneConfig> = {
  property: {
    heading: "Declared properties",
    copy: CONTRACT_COPY,
    catalogKey: PROPERTIES_KEY,
    listCatalog: listProperties,
    key: classifierPropertiesKey,
    list: classifierProperties,
    set: setClassifierProperty,
    del: deleteClassifierProperty,
    nameOf: (line) => line.property_type_name ?? "",
    pickerLabel: "Property to declare",
    pickerPrompt: "Declare a property…",
    addDefaultLabel: "Default for the new property",
    declareLabel: "Declare property",
    exhausted: "Every catalog property is already declared.",
  },
  metric: {
    heading: "Declared metrics",
    copy: METRIC_CONTRACT_COPY,
    catalogKey: METRICS_KEY,
    listCatalog: listMetricTypes,
    key: classifierMetricsKey,
    list: classifierMetrics,
    set: setClassifierMetric,
    del: deleteClassifierMetric,
    nameOf: (line) => line.metric_type_name ?? "",
    pickerLabel: "Metric to declare",
    pickerPrompt: "Declare a metric…",
    addDefaultLabel: "Default for the new metric",
    declareLabel: "Declare metric",
    exhausted: "Every catalog metric is already declared.",
  },
};

// contractRow is one line of the contract joined to its catalog entry, so the
// row can show the display name and data type alongside the declared default.
type ContractRow = { name: string; line: contractLine; meta?: catalogRow };

// dataTypeOf falls back to string for an entry that is not in the catalog read
// (a race, or one the caller cannot see): a text default still round-trips.
const dataTypeOf = (meta?: catalogRow): ValueType => (meta?.data_type as ValueType) ?? "string";

export default function ContractEditor(props: {
  classifier: ClassifierKind;
  id: string;
  official: boolean;
  lane?: ContractLane;
}): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const lane = () => LANES[props.lane ?? "property"];
  const copy = () => lane().copy[props.classifier];
  const key = () => lane().key(props.classifier, props.id);
  const q = useQuery(() => ({
    queryKey: key(),
    queryFn: () => lane().list(props.classifier, props.id),
    // Lines are edited inline; a background window-focus refetch would rebuild the
    // list and discard an in-progress edit.
    refetchOnWindowFocus: false,
  }));
  const catalog = useQuery(() => ({ queryKey: lane().catalogKey, queryFn: () => lane().listCatalog() }));

  const byName = createMemo(() => new Map((catalog.data ?? []).map((p) => [p.name, p])));
  const rows = createMemo<ContractRow[]>(() =>
    (q.data ?? [])
      .map((line) => ({ name: lane().nameOf(line), line }))
      .sort((a, b) => a.name.localeCompare(b.name))
      .map((r) => ({ ...r, meta: byName().get(r.name) })),
  );

  // A read-only contract: an official classifier is seed-owned, and declaring is
  // the classifier's own :update (withdrawing its :delete, as the server gates them).
  const resource = () => CLASSIFIER_RESOURCE[props.classifier];
  const canDeclare = () => !props.official && can(me.data, resource(), "update");
  const canWithdraw = () => !props.official && can(me.data, resource(), "delete");

  // The catalog minus what the classifier already declares: an entry is declared
  // at most once, so the picker cannot offer a duplicate.
  const declarable = createMemo(() => {
    const taken = new Set((q.data ?? []).map((r) => lane().nameOf(r)));
    return [...(catalog.data ?? [])].filter((p) => !taken.has(p.name)).sort((a, b) => a.name.localeCompare(b.name));
  });

  const [err, setErr] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);
  // The line name open for editing (one at a time), and its draft.
  const [editing, setEditing] = createSignal<string | null>(null);
  const [draftDefault, setDraftDefault] = createSignal("");
  const [draftRequired, setDraftRequired] = createSignal(false);
  // The add row's draft: the picked entry, its default, and its required flag.
  const [addName, setAddName] = createSignal("");
  const [addDefault, setAddDefault] = createSignal("");
  const [addRequired, setAddRequired] = createSignal(false);

  function openEdit(r: ContractRow) {
    setEditing(r.name);
    setDraftDefault(displayValue(r.line.default_value));
    setDraftRequired(r.line.required);
    setErr(null);
  }

  function resetAdd() {
    setAddName("");
    setAddDefault("");
    setAddRequired(false);
  }

  // buildBody coerces the typed default out of the text draft: blank means no
  // default (the field is omitted), and a value that will not parse is reported
  // instead of being sent malformed.
  function buildBody(dataType: ValueType, text: string, required: boolean): SetClassifierProperty | null {
    const trimmed = text.trim();
    if (trimmed === "") return { required };
    try {
      return { required, default_value: parseInput(dataType, trimmed) };
    } catch {
      setErr(`"${trimmed}" is not a valid ${dataType} value.`);
      return null;
    }
  }

  async function write(name: string, body: SetClassifierProperty, after: () => void) {
    setBusy(true);
    setErr(null);
    try {
      await lane().set(props.classifier, props.id, name, body);
      await qc.invalidateQueries({ queryKey: key() });
      after();
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  async function saveEdit(r: ContractRow) {
    setErr(null);
    const body = buildBody(dataTypeOf(r.meta), draftDefault(), draftRequired());
    if (!body) return;
    await write(r.name, body, () => setEditing(null));
  }

  async function declare() {
    const name = addName();
    if (!name) return;
    setErr(null);
    const body = buildBody(dataTypeOf(byName().get(name)), addDefault(), addRequired());
    if (!body) return;
    await write(name, body, resetAdd);
  }

  async function withdraw(name: string) {
    if (!confirm(copy().confirm(name))) return;
    setBusy(true);
    setErr(null);
    try {
      await lane().del(props.classifier, props.id, name);
      await qc.invalidateQueries({ queryKey: key() });
      if (editing() === name) setEditing(null);
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div class="flex flex-col gap-2">
      <div class="flex items-baseline justify-between gap-2">
        <span class="eyebrow">{lane().heading}</span>
        <span class="shrink-0 text-[10.5px] text-base-content/40">
          {props.official ? "seed-owned, read-only" : copy().hint}
        </span>
      </div>
      <p class="text-[11px] text-base-content/50">{copy().lede}</p>

      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft py-1.5 text-xs"><span>{err()}</span></div>
      </Show>
      <Show when={q.error}>
        <div role="alert" class="alert alert-error alert-soft py-1.5 text-xs"><span>{describeError(q.error)}</span></div>
      </Show>

      <Show when={!q.isLoading && !q.error && !rows().length}>
        <p class="text-sm text-base-content/50">{copy().empty}</p>
      </Show>

      <Show when={rows().length}>
        <div class="divide-y divide-base-300 overflow-hidden rounded-box border border-base-300">
          <For each={rows()}>
            {(r) => (
              <div class="flex flex-col gap-1 px-3 py-2">
                <div class="flex items-center gap-2">
                  <span class="min-w-0 flex-1 truncate">
                    <span class="font-data text-sm">{r.name}</span>
                    <Show when={r.meta?.display_name}>
                      <span class="ml-2 text-[11px] text-base-content/50">{r.meta?.display_name}</span>
                    </Show>
                  </span>
                  <span class="badge badge-ghost badge-sm shrink-0 font-data">{r.meta?.data_type ?? "string"}</span>
                  <Show when={canDeclare() && editing() !== r.name}>
                    <Button
                      square
                      size="xs"
                      icon={Pencil}
                      label={`Edit ${r.name}`}
                      title="Edit"
                      onClick={() => openEdit(r)}
                    />
                  </Show>
                  <Show when={canWithdraw()}>
                    <Button
                      square
                      size="xs"
                      icon={Trash}
                      label={`Withdraw ${r.name}`}
                      title="Withdraw"
                      disabled={busy()}
                      onClick={() => withdraw(r.name)}
                    />
                  </Show>
                </div>

                <Show
                  when={editing() === r.name}
                  fallback={
                    <div class="flex items-center gap-2 text-[11px]">
                      <span class="text-base-content/40">default</span>
                      <Show
                        when={r.line.default_value !== null && r.line.default_value !== undefined}
                        fallback={<span class="text-base-content/40 italic">no default</span>}
                      >
                        <span class="min-w-0 truncate font-data text-base-content/70">{displayValue(r.line.default_value)}</span>
                      </Show>
                      <span class="flex-1" />
                      <Show
                        when={r.line.required}
                        fallback={<span class="text-base-content/40">optional</span>}
                      >
                        <span class="badge badge-outline badge-sm">required</span>
                      </Show>
                    </div>
                  }
                >
                  <div class="flex items-center gap-2">
                    <input
                      class="input input-bordered input-sm min-w-0 flex-1 font-data"
                      placeholder={`default (${dataTypeOf(r.meta)}), blank for none`}
                      aria-label={`Default for ${r.name}`}
                      value={draftDefault()}
                      onInput={(e) => setDraftDefault(e.currentTarget.value)}
                    />
                    <label class="flex shrink-0 items-center gap-1.5 text-[11px] text-base-content/60">
                      <input
                        type="checkbox"
                        class="checkbox checkbox-xs"
                        checked={draftRequired()}
                        onChange={(e) => setDraftRequired(e.currentTarget.checked)}
                      />
                      required
                    </label>
                    <Button square size="xs" intent="action" icon={Check} label={`Save ${r.name}`} title="Save" disabled={busy()} onClick={() => saveEdit(r)} />
                    <Button square size="xs" icon={X} label="Cancel" title="Cancel" onClick={() => setEditing(null)} />
                  </div>
                </Show>
              </div>
            )}
          </For>
        </div>
      </Show>

      <Show when={canDeclare()}>
        <Show
          when={declarable().length}
          fallback={<span class="text-[11px] text-base-content/40">{lane().exhausted}</span>}
        >
          <div class="flex flex-col gap-1.5 rounded-box border border-dashed border-base-300 p-2.5">
            <select
              class="select select-bordered select-sm w-full"
              aria-label={lane().pickerLabel}
              value={addName()}
              onChange={(e) => setAddName(e.currentTarget.value)}
            >
              <option value="">{lane().pickerPrompt}</option>
              <For each={declarable()}>
                {(p) => <option value={p.name}>{p.display_name ? `${p.name} (${p.display_name})` : p.name}</option>}
              </For>
            </select>
            <Show when={addName()}>
              <div class="flex items-center gap-2">
                <input
                  class="input input-bordered input-sm min-w-0 flex-1 font-data"
                  placeholder={`default (${dataTypeOf(byName().get(addName()))}), blank for none`}
                  aria-label={lane().addDefaultLabel}
                  value={addDefault()}
                  onInput={(e) => setAddDefault(e.currentTarget.value)}
                />
                <label class="flex shrink-0 items-center gap-1.5 text-[11px] text-base-content/60">
                  <input
                    type="checkbox"
                    class="checkbox checkbox-xs"
                    checked={addRequired()}
                    onChange={(e) => setAddRequired(e.currentTarget.checked)}
                  />
                  required
                </label>
                <Button square size="xs" intent="action" icon={Plus} label={lane().declareLabel} title="Declare" disabled={busy()} onClick={declare} />
                <Button square size="xs" icon={X} label="Cancel declaration" title="Cancel" onClick={resetAdd} />
              </div>
            </Show>
          </div>
        </Show>
      </Show>
    </div>
  );
}
