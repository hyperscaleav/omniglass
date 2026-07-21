import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import { Check, Pencil, Plus, Trash, X } from "./icons";
import { PROPERTIES_KEY, listProperties, type PropertyRow } from "../lib/properties";
import {
  productPropertiesKey,
  productProperties,
  setProductProperty,
  deleteProductProperty,
  type ProductProperty,
  type SetProductProperty,
} from "../lib/product_properties";
import { displayValue, parseInput, type ValueType } from "../lib/variables";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";

// ProductContractEditor is the product detail-blade panel for curating a product's
// declared-properties contract: which catalog properties every instance of the
// product exposes, and what each one defaults to. A component of the product
// resolves a declared property to its own override, or to the default declared
// here; required means the component must resolve it to a value.
//
// Each line is addressed by property name, so a write is a PUT (idempotent: an
// edit revises the line in place) and a withdraw is a DELETE. Writes are immediate,
// like the tag panel, so the panel has no Save of its own and does not contend with
// the blade's edit slot (which the product's core facts already own). Declaring
// needs product:update, withdrawing product:delete, and an official (seed-owned)
// product's contract is read-only: the list renders, the controls do not.

// contractRow is one line of the contract joined to its catalog property, so the
// row can show the display name and data type alongside the declared default.
type ContractRow = { line: ProductProperty; meta?: PropertyRow };

// dataTypeOf falls back to string for a property that is not in the catalog read
// (a race, or a property the caller cannot see): a text default still round-trips.
const dataTypeOf = (meta?: PropertyRow): ValueType => (meta?.data_type as ValueType) ?? "string";

export default function ProductContractEditor(props: { productId: string; official: boolean }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const q = useQuery(() => ({
    queryKey: productPropertiesKey(props.productId),
    queryFn: () => productProperties(props.productId),
    // Lines are edited inline; a background window-focus refetch would rebuild the
    // list and discard an in-progress edit.
    refetchOnWindowFocus: false,
  }));
  const catalog = useQuery(() => ({ queryKey: PROPERTIES_KEY, queryFn: listProperties }));

  const byName = createMemo(() => new Map((catalog.data ?? []).map((p) => [p.name, p])));
  const rows = createMemo<ContractRow[]>(() =>
    [...(q.data ?? [])]
      .sort((a, b) => a.property_name.localeCompare(b.property_name))
      .map((line) => ({ line, meta: byName().get(line.property_name) })),
  );

  // A read-only contract: an official product is seed-owned, and declaring is the
  // product's own :update (withdrawing its :delete, as the server gates them).
  const canDeclare = () => !props.official && can(me.data, "product", "update");
  const canWithdraw = () => !props.official && can(me.data, "product", "delete");

  // The catalog minus what the product already declares: a property is declared at
  // most once, so the picker cannot offer a duplicate.
  const declarable = createMemo(() => {
    const taken = new Set((q.data ?? []).map((r) => r.property_name));
    return [...(catalog.data ?? [])].filter((p) => !taken.has(p.name)).sort((a, b) => a.name.localeCompare(b.name));
  });

  const [err, setErr] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);
  // The property name whose line is open for editing (one at a time), and its draft.
  const [editing, setEditing] = createSignal<string | null>(null);
  const [draftDefault, setDraftDefault] = createSignal("");
  const [draftRequired, setDraftRequired] = createSignal(false);
  // The add row's draft: the picked property, its default, and its required flag.
  const [addName, setAddName] = createSignal("");
  const [addDefault, setAddDefault] = createSignal("");
  const [addRequired, setAddRequired] = createSignal(false);

  function openEdit(r: ContractRow) {
    setEditing(r.line.property_name);
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
  function buildBody(dataType: ValueType, text: string, required: boolean): SetProductProperty | null {
    const trimmed = text.trim();
    if (trimmed === "") return { required };
    try {
      return { required, default_value: parseInput(dataType, trimmed) };
    } catch {
      setErr(`"${trimmed}" is not a valid ${dataType} value.`);
      return null;
    }
  }

  async function write(property: string, body: SetProductProperty, after: () => void) {
    setBusy(true);
    setErr(null);
    try {
      await setProductProperty(props.productId, property, body);
      await qc.invalidateQueries({ queryKey: productPropertiesKey(props.productId) });
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
    await write(r.line.property_name, body, () => setEditing(null));
  }

  async function declare() {
    const name = addName();
    if (!name) return;
    setErr(null);
    const body = buildBody(dataTypeOf(byName().get(name)), addDefault(), addRequired());
    if (!body) return;
    await write(name, body, resetAdd);
  }

  async function withdraw(property: string) {
    if (!confirm(`Withdraw "${property}" from this product's contract?`)) return;
    setBusy(true);
    setErr(null);
    try {
      await deleteProductProperty(props.productId, property);
      await qc.invalidateQueries({ queryKey: productPropertiesKey(props.productId) });
      if (editing() === property) setEditing(null);
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div class="flex flex-col gap-2">
      <div class="flex items-baseline justify-between gap-2">
        <span class="eyebrow">Declared properties</span>
        <span class="shrink-0 text-[10.5px] text-base-content/40">
          {props.official ? "seed-owned, read-only" : "the product contract"}
        </span>
      </div>
      <p class="text-[11px] text-base-content/50">
        A component of this product inherits every property declared here, resolved to the default below unless the component overrides it.
      </p>

      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft py-1.5 text-xs"><span>{err()}</span></div>
      </Show>
      <Show when={q.error}>
        <div role="alert" class="alert alert-error alert-soft py-1.5 text-xs"><span>{describeError(q.error)}</span></div>
      </Show>

      <Show when={!q.isLoading && !q.error && !rows().length}>
        <p class="text-sm text-base-content/50">This product declares no properties.</p>
      </Show>

      <Show when={rows().length}>
        <div class="divide-y divide-base-300 overflow-hidden rounded-box border border-base-300">
          <For each={rows()}>
            {(r) => (
              <div class="flex flex-col gap-1 px-3 py-2">
                <div class="flex items-center gap-2">
                  <span class="min-w-0 flex-1 truncate">
                    <span class="font-data text-sm">{r.line.property_name}</span>
                    <Show when={r.meta?.display_name}>
                      <span class="ml-2 text-[11px] text-base-content/50">{r.meta?.display_name}</span>
                    </Show>
                  </span>
                  <span class="badge badge-ghost badge-sm shrink-0 font-data">{r.meta?.data_type ?? "string"}</span>
                  <Show when={canDeclare() && editing() !== r.line.property_name}>
                    <Button
                      square
                      size="xs"
                      icon={Pencil}
                      label={`Edit ${r.line.property_name}`}
                      title="Edit"
                      onClick={() => openEdit(r)}
                    />
                  </Show>
                  <Show when={canWithdraw()}>
                    <Button
                      square
                      size="xs"
                      icon={Trash}
                      label={`Withdraw ${r.line.property_name}`}
                      title="Withdraw"
                      disabled={busy()}
                      onClick={() => withdraw(r.line.property_name)}
                    />
                  </Show>
                </div>

                <Show
                  when={editing() === r.line.property_name}
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
                      aria-label={`Default for ${r.line.property_name}`}
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
                    <Button square size="xs" intent="action" icon={Check} label={`Save ${r.line.property_name}`} title="Save" disabled={busy()} onClick={() => saveEdit(r)} />
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
          fallback={<span class="text-[11px] text-base-content/40">Every catalog property is already declared.</span>}
        >
          <div class="flex flex-col gap-1.5 rounded-box border border-dashed border-base-300 p-2.5">
            <select
              class="select select-bordered select-sm w-full"
              aria-label="Property to declare"
              value={addName()}
              onChange={(e) => setAddName(e.currentTarget.value)}
            >
              <option value="">Declare a property…</option>
              <For each={declarable()}>
                {(p) => <option value={p.name}>{p.display_name ? `${p.name} (${p.display_name})` : p.name}</option>}
              </For>
            </select>
            <Show when={addName()}>
              <div class="flex items-center gap-2">
                <input
                  class="input input-bordered input-sm min-w-0 flex-1 font-data"
                  placeholder={`default (${dataTypeOf(byName().get(addName()))}), blank for none`}
                  aria-label="Default for the new property"
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
                <Button square size="xs" intent="action" icon={Plus} label="Declare property" title="Declare" disabled={busy()} onClick={declare} />
                <Button square size="xs" icon={X} label="Cancel declaration" title="Cancel" onClick={resetAdd} />
              </div>
            </Show>
          </div>
        </Show>
      </Show>
    </div>
  );
}
