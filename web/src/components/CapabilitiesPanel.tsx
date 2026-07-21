import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import { Ban, Plus, RotateCcw } from "./icons";
import { describeError } from "../lib/format";
import { CAPABILITIES_KEY, listCapabilities } from "../lib/capabilities";
import { PRODUCTS_KEY, listProducts } from "../lib/products";
import {
  clearComponentCapability,
  componentCapabilities,
  componentCapabilitiesKey,
  setComponentCapability,
  splitCapabilities,
  type CapabilityLine,
} from "../lib/component_capabilities";

// CapabilitiesPanel lists what a component can do, resolved. A CAPABILITY
// ("microphone", "speaker") is what a system ROLE checks before letting the
// component fill it, so this panel is the other half of the roles surface: the
// role names what it requires, the component says what it provides.
//
// The set is resolved from two origins, and the panel keeps them legible because
// the write differs per origin: what the component's PRODUCT declares (every
// component of that product has it, and this component can suppress it) and what
// this component declares of its OWN (either an addition its product does not
// claim, or the suppression of one it does). A suppressed capability is not in the
// resolved set, which is exactly what suppression means, so it is still listed,
// struck through, because it is the only way to find and restore it.
//
// Three writes, one per origin: suppress an inherited one, add one of the
// component's own, or clear the component's fact so it falls back to whatever the
// product declares. Writes are immediate, like the tag panel, so the panel has no
// Save of its own; the caller passes canUpdate (the component detail computes it
// as "in edit mode AND holding component:update"), which keeps view read-only per
// the console invariant.

const ORIGIN_BADGE: Record<CapabilityLine["origin"], { cls: string; label: string }> = {
  product: { cls: "badge-ghost", label: "from the product" },
  component: { cls: "badge-outline", label: "on this component" },
  suppressed: { cls: "badge-warning badge-soft", label: "suppressed" },
};

export default function CapabilitiesPanel(props: {
  component: string;
  // The product the component is an instance of, if any: what it declares is what
  // the resolved set is read against.
  productId?: string;
  canUpdate: boolean;
}): JSX.Element {
  const qc = useQueryClient();
  const key = () => componentCapabilitiesKey(props.component);
  const q = useQuery(() => ({
    queryKey: key(),
    queryFn: () => componentCapabilities(props.component),
    refetchOnWindowFocus: false,
  }));
  const products = useQuery(() => ({ queryKey: PRODUCTS_KEY, queryFn: listProducts }));
  const catalog = useQuery(() => ({ queryKey: CAPABILITIES_KEY, queryFn: listCapabilities }));

  // What the product declares. Unknown (no product, or a product the caller cannot
  // see) reads as declaring nothing, so every resolved capability shows as the
  // component's own, which is the truthful reading for a productless component.
  const declared = createMemo<string[]>(
    () => (props.productId ? (products.data ?? []).find((p) => p.id === props.productId)?.capabilities ?? [] : []),
  );
  const lines = createMemo<CapabilityLine[]>(() => splitCapabilities(q.data ?? [], declared()));
  const nameOf = (id: string) => (catalog.data ?? []).find((c) => c.id === id)?.display_name ?? "";

  // The registry minus everything already on the component, suppressed included:
  // restoring a suppressed capability is a clear, not an add.
  const addable = createMemo(() => {
    const taken = new Set(lines().map((l) => l.id));
    return [...(catalog.data ?? [])].filter((c) => !taken.has(c.id)).sort((a, b) => a.display_name.localeCompare(b.display_name));
  });

  const [err, setErr] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);
  const [adding, setAdding] = createSignal("");

  async function run(write: () => Promise<void>, after?: () => void) {
    setBusy(true);
    setErr(null);
    try {
      await write();
      await qc.invalidateQueries({ queryKey: key() });
      after?.();
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  const add = () => {
    const id = adding();
    if (!id) return;
    return run(() => setComponentCapability(props.component, id, true), () => setAdding(""));
  };
  const suppress = (id: string) => run(() => setComponentCapability(props.component, id, false));
  const clear = (id: string) => run(() => clearComponentCapability(props.component, id));

  return (
    <div class="flex flex-col gap-2">
      <div class="flex items-baseline justify-between gap-2">
        <span class="eyebrow">Capabilities</span>
        <span class="shrink-0 text-[10.5px] text-base-content/40">the product's, plus this component's own</span>
      </div>
      <p class="text-[11px] text-base-content/50">
        What this component can do, and so which system roles it may fill. A role checks every capability it requires
        against this set.
      </p>

      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft py-1.5 text-xs"><span>{err()}</span></div>
      </Show>
      <Show when={q.error}>
        <div role="alert" class="alert alert-error alert-soft py-1.5 text-xs"><span>{describeError(q.error)}</span></div>
      </Show>

      <Show when={!q.isLoading && !q.error && !lines().length}>
        <p class="text-sm text-base-content/50">
          This component provides nothing yet: its product declares no capabilities, and it declares none of its own.
        </p>
      </Show>

      <Show when={lines().length}>
        <div class="divide-y divide-base-300 overflow-hidden rounded-box border border-base-300">
          <For each={lines()}>
            {(l) => (
              <div class="flex items-center gap-2 px-3 py-2">
                <span
                  class="min-w-0 flex-1 truncate font-data text-sm"
                  classList={{ "text-base-content/40 line-through": l.origin === "suppressed" }}
                >
                  {l.id}
                  <Show when={nameOf(l.id)}>
                    <span class="ml-2 font-sans text-[11px] text-base-content/50">{nameOf(l.id)}</span>
                  </Show>
                </span>
                <span class={`badge badge-sm shrink-0 ${ORIGIN_BADGE[l.origin].cls}`}>{ORIGIN_BADGE[l.origin].label}</span>
                <Show when={props.canUpdate}>
                  {/* Suppressing an inherited capability is a fact the component
                      states; clearing that fact (an addition or a suppression)
                      hands the answer back to the product. */}
                  <Show when={l.origin === "product"}>
                    <Button square size="xs" icon={Ban} label={`Suppress ${l.id}`} title="Suppress" disabled={busy()} onClick={() => void suppress(l.id)} />
                  </Show>
                  <Show when={l.origin !== "product"}>
                    <Button
                      square
                      size="xs"
                      icon={RotateCcw}
                      label={`Clear ${l.id}`}
                      title={l.origin === "suppressed" ? "Clear back to the product default" : "Remove from this component"}
                      disabled={busy()}
                      onClick={() => void clear(l.id)}
                    />
                  </Show>
                </Show>
              </div>
            )}
          </For>
        </div>
      </Show>

      <Show when={props.canUpdate}>
        <Show
          when={addable().length}
          fallback={<span class="text-[11px] text-base-content/40">Every registered capability is already on this component.</span>}
        >
          <div class="flex items-center gap-1.5 rounded-box border border-dashed border-base-300 p-2.5">
            <select
              class="select select-bordered select-sm min-w-0 flex-1"
              aria-label="Capability to add"
              value={adding()}
              onChange={(e) => setAdding(e.currentTarget.value)}
            >
              <option value="">Add a capability…</option>
              <For each={addable()}>{(c) => <option value={c.id}>{c.display_name} ({c.id})</option>}</For>
            </select>
            <Button square size="sm" intent="action" icon={Plus} label="Add capability" title="Add" disabled={busy() || !adding()} onClick={() => void add()} />
          </div>
        </Show>
      </Show>
    </div>
  );
}
