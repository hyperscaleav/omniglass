import { For, Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import KVStacked from "../components/KVStacked";
import Button from "../components/Button";
import { DrawerFooter } from "../components/Drawer";
import { Plus } from "../components/icons";
import {
  type Product,
  type ProductKind,
  PRODUCTS_KEY,
  listProducts,
  createProduct,
  updateProduct,
  deleteProduct,
} from "../lib/products";
import { type Vendor, VENDORS_KEY, listVendors } from "../lib/vendors";
import { type Driver, DRIVERS_KEY, listDrivers } from "../lib/drivers";
import { type Capability, CAPABILITIES_KEY, listCapabilities } from "../lib/capabilities";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Products: the product catalog (the model a component is an instance of, e.g.
// "Crestron TSW-1070"), on the flat FlatList surface. A product is addressed by
// its id (a kebab id, create-only); official (seed-owned) rows are read-only,
// same as the Types catalog's official rows: no Edit pencil, no Delete. A
// product carries a kind (device/app/service/vm), an optional vendor and driver
// (picked from those registries), and a set of capability ids it exposes.

const PRODUCT_KINDS: ProductKind[] = ["device", "app", "service", "vm"];

function officialBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

function kindBadge(kind: string): JSX.Element {
  return <span class="badge badge-ghost badge-sm">{kind}</span>;
}

function refCell(id?: string): JSX.Element {
  return <span class="font-data text-xs text-base-content/60">{id || "—"}</span>;
}

const columns: FlatColumn<Product>[] = [
  { key: "id", label: "Id", sortVal: (p) => p.id, cell: (p) => <span class="font-data font-semibold">{p.id}</span> },
  { key: "display_name", label: "Display name", sortVal: (p) => p.display_name, cell: (p) => <span>{p.display_name}</span> },
  { key: "vendor", label: "Vendor", width: "150px", sortVal: (p) => p.vendor_id ?? "", cell: (p) => refCell(p.vendor_id) },
  { key: "driver", label: "Driver", width: "150px", sortVal: (p) => p.driver_id ?? "", cell: (p) => refCell(p.driver_id) },
  { key: "kind", label: "Kind", width: "110px", sortVal: (p) => p.kind, cell: (p) => kindBadge(p.kind) },
  { key: "official", label: "Origin", width: "100px", sortVal: (p) => String(p.official), cell: (p) => officialBadge(p.official) },
];

export default function Products() {
  const me = useMe();
  const products = useQuery(() => ({ queryKey: PRODUCTS_KEY, queryFn: listProducts }));

  const rows = createMemo(() =>
    [...(products.data ?? [])].sort((a, b) => a.display_name.localeCompare(b.display_name) || a.id.localeCompare(b.id)),
  );

  return (
    <FlatList<Product>
      config={{
        entity: { name: "product", plural: "products" },
        rows,
        loading: () => products.isPending,
        error: () => products.error,
        filterKeys: [
          { key: "name", type: "string", hint: "substring", get: (p) => `${p.id} ${p.display_name}`, values: () => [] },
          { key: "kind", type: "string", hint: "exact", get: (p) => p.kind, values: () => PRODUCT_KINDS },
          { key: "vendor", type: "string", hint: "exact", get: (p) => p.vendor_id ?? "", values: () => [] },
          { key: "official", type: "string", hint: "exact", get: (p) => (p.official ? "official" : "custom"), values: () => ["official", "custom"] },
        ],
        filterPlaceholder: "filter products by id, name…",
        columns,
        empty: "No products yet.",
        // Address a row by id: the write paths key on id, and it is globally unique.
        rowId: (p) => p.id,
        blades: { registry: { product: productBlade }, rootKind: "product" },
        create: can(me.data, "product", "create")
          ? { label: "New product", can: () => can(me.data, "product", "create"), body: (ctx) => <CreateProductForm onCreated={ctx.close} /> }
          : undefined,
      }}
    />
  );
}

// productBlade renders an id on the shared blade stack. An official row is
// read-only (no pencil, no destructive action); a custom row carries Edit +
// Delete.
export const productBlade: BladeDef = {
  Title: (p) => <ProductBladeTitle id={p.id} />,
  Body: (p) => <ProductBladeBody id={p.id} />,
};

function useProductRow(id: string): () => Product | undefined {
  const products = useQuery(() => ({ queryKey: PRODUCTS_KEY, queryFn: listProducts }));
  return () => (products.data ?? []).find((p) => p.id === id);
}

function ProductBladeTitle(p: { id: string }): JSX.Element {
  const row = useProductRow(p.id);
  return <span class="font-data">{row()?.id ?? p.id}</span>;
}

function ProductBladeBody(p: { id: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const row = useProductRow(p.id);
  const [err, setErr] = createSignal<string | null>(null);
  const [displayName, setDisplayName] = createSignal("");
  const [kind, setKind] = createSignal<ProductKind>("device");
  const [vendorId, setVendorId] = createSignal("");
  const [driverId, setDriverId] = createSignal("");
  const [capabilities, setCapabilities] = createSignal<string[]>([]);

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    const r = row();
    setDisplayName(r?.display_name ?? "");
    setKind(r?.kind ?? "device");
    setVendorId(r?.vendor_id ?? "");
    setDriverId(r?.driver_id ?? "");
    setCapabilities(r?.capabilities ?? []);
    setErr(null);
  }));

  async function removeProduct() {
    const r = row();
    if (!r) return;
    if (!confirm(`Delete product "${r.id}"?`)) return;
    setErr(null);
    try {
      await deleteProduct(r.id);
      blades.close();
      await qc.invalidateQueries({ queryKey: PRODUCTS_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const r = row();
    if (!r) return;
    setErr(null);
    try {
      await updateProduct(r.id, {
        display_name: displayName(),
        kind: kind(),
        vendor_id: vendorId() || undefined,
        driver_id: driverId() || undefined,
        capabilities: capabilities(),
      });
      await qc.invalidateQueries({ queryKey: PRODUCTS_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  edit.bind({
    editable: () => !!row() && !row()!.official && can(me.data, "product", "update"),
    save,
    destructive: () =>
      row() && !row()!.official && can(me.data, "product", "delete")
        ? { label: "Delete", tone: "danger", onClick: removeProduct }
        : undefined,
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Product not found.</p>}>
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
            <span class="eyebrow">Kind</span>
            <Show
              when={edit.editing()}
              fallback={<div class="input input-bordered flex items-center text-sm">{kindBadge(r().kind)}</div>}
            >
              <select class="select select-bordered w-full" value={kind()} onChange={(e) => setKind(e.currentTarget.value as ProductKind)}>
                <For each={PRODUCT_KINDS}>{(k) => <option value={k}>{k}</option>}</For>
              </select>
            </Show>
          </div>
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Vendor</span>
            <Show
              when={edit.editing()}
              fallback={<div class="input input-bordered flex items-center text-sm font-data">{r().vendor_id || "—"}</div>}
            >
              <VendorSelect value={vendorId()} onChange={setVendorId} />
            </Show>
          </div>
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Driver</span>
            <Show
              when={edit.editing()}
              fallback={<div class="input input-bordered flex items-center text-sm font-data">{r().driver_id || "—"}</div>}
            >
              <DriverSelect value={driverId()} onChange={setDriverId} />
            </Show>
          </div>
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Capabilities</span>
            <Show
              when={edit.editing()}
              fallback={
                <Show when={r().capabilities.length} fallback={<div class="input input-bordered flex items-center text-sm">—</div>}>
                  <div class="flex flex-wrap gap-1.5">
                    <For each={r().capabilities}>{(c) => <span class="badge badge-ghost badge-sm font-data">{c}</span>}</For>
                  </div>
                </Show>
              }
            >
              <CapabilitiesPicker value={capabilities()} onChange={setCapabilities} />
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

// CreateProductForm: name the id (a kebab id, immutable after creation), set the
// display name and kind; vendor, driver, and capabilities are optional.
export function CreateProductForm(p: { onCreated: (id: string) => void }): JSX.Element {
  const qc = useQueryClient();
  const [id, setId] = createSignal("");
  const [displayName, setDisplayName] = createSignal("");
  const [kind, setKind] = createSignal<ProductKind>("device");
  const [vendorId, setVendorId] = createSignal("");
  const [driverId, setDriverId] = createSignal("");
  const [capabilities, setCapabilities] = createSignal<string[]>([]);
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  async function submit(e: Event) {
    e.preventDefault();
    setBusy(true);
    setFormErr(null);
    try {
      await createProduct({
        id: id().trim(),
        display_name: displayName().trim(),
        kind: kind(),
        vendor_id: vendorId() || undefined,
        driver_id: driverId() || undefined,
        capabilities: capabilities().length ? capabilities() : undefined,
      });
      await qc.invalidateQueries({ queryKey: PRODUCTS_KEY });
      p.onCreated(id().trim());
    } catch (er) {
      setFormErr(describeError(er));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form class="flex min-h-full flex-col gap-4" onSubmit={submit}>
      <Show when={formErr()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{formErr()}</span></div>
      </Show>
      <Field label="Id" hint="A kebab id, e.g. crestron-tsw-1070.">
        <input class="input input-bordered w-full font-data" value={id()} placeholder="crestron-tsw-1070" onInput={(e) => setId(e.currentTarget.value)} />
      </Field>
      <Field label="Display name">
        <input class="input input-bordered w-full" value={displayName()} placeholder="Crestron TSW-1070" onInput={(e) => setDisplayName(e.currentTarget.value)} />
      </Field>
      <Field label="Kind" hint="What class of thing the product is.">
        <select class="select select-bordered w-full" value={kind()} onChange={(e) => setKind(e.currentTarget.value as ProductKind)}>
          <For each={PRODUCT_KINDS}>{(k) => <option value={k}>{k}</option>}</For>
        </select>
      </Field>
      <Field label="Vendor" hint="Who makes it. Optional.">
        <VendorSelect value={vendorId()} onChange={setVendorId} />
      </Field>
      <Field label="Driver" hint="How its signals are collected. Optional.">
        <DriverSelect value={driverId()} onChange={setDriverId} />
      </Field>
      {/* Not wrapped in Field: Field's root is a <label>, and a picker of one
          <label> per checkbox nested inside it is invalid HTML that forwards a
          click on the heading straight to the first checkbox. */}
      <div class="flex flex-col gap-1.5">
        <span class="text-[12px] font-medium text-base-content/70">Capabilities</span>
        <CapabilitiesPicker value={capabilities()} onChange={setCapabilities} />
        <span class="text-[11px] text-base-content/40">What the product can do. Optional.</span>
      </div>
      <DrawerFooter>
        <Button type="submit" intent="action" icon={Plus} disabled={busy() || !id().trim() || !displayName().trim()}>Create product</Button>
      </DrawerFooter>
    </form>
  );
}

// VendorSelect: a vendor picker over the vendor registry, with a "None" option
// (a product need not name a vendor). Stores the vendor id.
function VendorSelect(p: { value: string; onChange: (v: string) => void }): JSX.Element {
  const vendors = useQuery(() => ({ queryKey: VENDORS_KEY, queryFn: listVendors }));
  const options = createMemo(() =>
    [...(vendors.data ?? [])].sort((a: Vendor, b: Vendor) => a.display_name.localeCompare(b.display_name)),
  );
  return (
    <select class="select select-bordered w-full" value={p.value} onChange={(e) => p.onChange(e.currentTarget.value)}>
      <option value="">None</option>
      <For each={options()}>{(v) => <option value={v.id}>{v.display_name}</option>}</For>
    </select>
  );
}

// DriverSelect: a driver picker over the driver registry, with a "None" option.
// Stores the driver id.
function DriverSelect(p: { value: string; onChange: (v: string) => void }): JSX.Element {
  const drivers = useQuery(() => ({ queryKey: DRIVERS_KEY, queryFn: listDrivers }));
  const options = createMemo(() =>
    [...(drivers.data ?? [])].sort((a: Driver, b: Driver) => a.display_name.localeCompare(b.display_name)),
  );
  return (
    <select class="select select-bordered w-full" value={p.value} onChange={(e) => p.onChange(e.currentTarget.value)}>
      <option value="">None</option>
      <For each={options()}>{(d) => <option value={d.id}>{d.display_name}</option>}</For>
    </select>
  );
}

// CapabilitiesPicker: a checkbox per capability in the registry, the set of
// capability ids a product exposes. Mirrors Types.tsx's AllowedParentsPicker.
// Each option is its own <label> (not nested inside another one), so a click on
// it only ever toggles that option's own checkbox.
function CapabilitiesPicker(p: { value: string[]; onChange: (v: string[]) => void }): JSX.Element {
  const caps = useQuery(() => ({ queryKey: CAPABILITIES_KEY, queryFn: listCapabilities }));
  const options = createMemo(() =>
    [...(caps.data ?? [])].sort((a: Capability, b: Capability) => a.display_name.localeCompare(b.display_name)),
  );
  function toggle(id: string) {
    p.onChange(p.value.includes(id) ? p.value.filter((x) => x !== id) : [...p.value, id]);
  }
  return (
    <div class="flex flex-col gap-1.5 rounded-box border border-base-300 p-2.5">
      <Show when={options().length} fallback={<span class="text-[11px] text-base-content/40">No capabilities in the registry yet.</span>}>
        <For each={options()}>
          {(c) => (
            <label class="flex items-center gap-2 text-sm">
              <input type="checkbox" class="checkbox checkbox-sm" checked={p.value.includes(c.id)} onChange={() => toggle(c.id)} />
              <span>{c.display_name}</span>
              <span class="font-data text-xs text-base-content/40">{c.id}</span>
            </label>
          )}
        </For>
      </Show>
    </div>
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
