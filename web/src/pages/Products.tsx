import { For, Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { Dynamic } from "solid-js/web";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import BladeTitle from "../components/BladeTitle";
import FieldRow from "../components/FieldRow";
import BladeField from "../components/BladeField";
import { identityColumn } from "../components/IdentityCell";
import KVStacked from "../components/KVStacked";
import { createIdentity, entityLabel } from "../lib/entities";
import { useFormActions } from "../lib/formactions";
import ProductContractEditor from "../components/ProductContractEditor";
import ComponentTypeSelect from "../components/ComponentTypeSelect";
import { Plus, resolveIcon } from "../components/icons";
import {
  type Product,
  type ProductKind,
  PRODUCTS_KEY,
  listProducts,
  createProduct,
  updateProduct,
  deleteProduct,
  resolveProductIcon,
} from "../lib/products";
import { COMPONENT_TYPES_KEY, listComponentTypes, componentTypeByName } from "../lib/component_types";
import { type Vendor, VENDORS_KEY, listVendors } from "../lib/vendors";
import { type Driver, DRIVERS_KEY, listDrivers } from "../lib/drivers";
import { useMe, can } from "../lib/auth";
import { registryLock } from "../lib/catalog";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Products: the product catalog (the model a component is an instance of, e.g.
// "Crestron TSW-1070"), on the flat FlatList surface. A product is addressed by
// its kebab name (ADR-0062: the uuid is identity, the name is what an operator
// reads and types); official rows are read-only,
// same as the Types catalog's official rows: the Edit / Delete pair greys. A
// product carries a kind (device/app/service; vm retired, folded into app,
// ADR-0086), a required component_type (the device-class genus every product
// is classified under, ADR-0085, picked from the Types tree; also what a
// system role's typed-slot guard checks, #626), an optional icon override
// (default: the type's icon), and an optional vendor and driver (picked from
// those registries).

const PRODUCT_KINDS: ProductKind[] = ["device", "app", "service"];

function officialBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

function kindBadge(kind: string): JSX.Element {
  return <span class="badge badge-ghost badge-sm">{kind}</span>;
}

// refCell prints a reference's name (never the uuid; ADR-0062).
function refCell(handle?: string): JSX.Element {
  return <span class="font-data text-xs text-base-content/60">{handle || "—"}</span>;
}

const columns: FlatColumn<Product>[] = [
  // One identity column, the shared cell: the label leads and the name
  // sits beneath it. A product whose label IS its handle renders it once.
  identityColumn<Product>(),
  { key: "vendor", label: "Vendor", width: "150px", sortVal: (p) => p.vendor ?? "", cell: (p) => refCell(p.vendor) },
  { key: "driver", label: "Driver", width: "150px", sortVal: (p) => p.driver ?? "", cell: (p) => refCell(p.driver) },
  { key: "kind", label: "Kind", width: "110px", sortVal: (p) => p.kind, cell: (p) => kindBadge(p.kind) },
  { key: "official", label: "Origin", width: "100px", sortVal: (p) => String(p.official), cell: (p) => officialBadge(p.official) },
];

export default function Products() {
  const me = useMe();
  const products = useQuery(() => ({ queryKey: PRODUCTS_KEY, queryFn: listProducts }));

  const rows = createMemo(() =>
    [...(products.data ?? [])].sort((a, b) => a.label.localeCompare(b.label) || a.name.localeCompare(b.name)),
  );

  return (
    <FlatList<Product>
      config={{
        entity: { name: "product", plural: "products" },
        rows,
        loading: () => products.isPending,
        error: () => products.error,
        filterKeys: [
          { key: "name", type: "string", hint: "substring", get: (p) => `${entityLabel(p)} ${p.name}`, values: () => [] },
          { key: "kind", type: "string", hint: "exact", get: (p) => p.kind, values: () => PRODUCT_KINDS },
          { key: "vendor", type: "string", hint: "exact", get: (p) => p.vendor ?? "", values: () => [] },
          { key: "official", type: "string", hint: "exact", get: (p) => (p.official ? "official" : "custom"), values: () => ["official", "custom"] },
        ],
        filterPlaceholder: "filter products by name…",
        columns,
        empty: "No products yet.",
        // Address a row by its kebab name: globally unique, and what the API
        // and CLI accept (the write paths resolve either form).
        rowId: (p) => p.name,
        blades: { registry: { product: productBlade }, rootKind: "product" },
        create: can(me.data, "product", "create")
          ? { label: "New product", can: () => can(me.data, "product", "create"), body: (ctx) => <CreateProductForm onCreated={ctx.select} /> }
          : undefined,
      }}
    />
  );
}

// productBlade renders an id on the shared blade stack. An official row is
// read-only (the Edit / Delete pair greys); a custom row carries Edit +
// Delete.
export const productBlade: BladeDef = {
  Title: (p) => <ProductBladeTitle id={p.id} />,
  Body: (p) => <ProductBladeBody id={p.id} />,
};

function useProductRow(id: string): () => Product | undefined {
  const products = useQuery(() => ({ queryKey: PRODUCTS_KEY, queryFn: listProducts }));
  return () => (products.data ?? []).find((p) => p.name === id);
}

function ProductBladeTitle(p: { id: string }): JSX.Element {
  return <BladeTitle row={useProductRow(p.id)} fallback={p.id} />;
}

function ProductBladeBody(p: { id: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const row = useProductRow(p.id);
  const [err, setErr] = createSignal<string | null>(null);
  const [label, setLabel] = createSignal("");
  const [kind, setKind] = createSignal<ProductKind>("device");
  const [componentType, setComponentType] = createSignal("");
  const [icon, setIcon] = createSignal("");
  const [vendorId, setVendorId] = createSignal("");
  const [driverId, setDriverId] = createSignal("");

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    const r = row();
    setLabel(r?.label ?? "");
    setKind(r?.kind ?? "device");
    setComponentType(r?.component_type ?? "");
    setIcon(r?.icon ?? "");
    setVendorId(r?.vendor ?? "");
    setDriverId(r?.driver ?? "");
    setErr(null);
  }));

  async function removeProduct() {
    const r = row();
    if (!r) return;
    if (!confirm(`Delete product "${r.name}"?`)) return;
    setErr(null);
    try {
      await deleteProduct(r.name);
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
      await updateProduct(r.name, {
        label: label(),
        kind: kind(),
        component_type: componentType(),
        icon: icon(),
        vendor_id: vendorId() || undefined,
        driver_id: driverId() || undefined,
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
    locked: () => registryLock(row(), me.data, "product"),
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Product not found.</p>}>
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
          <BladeField label="Kind" read={kindBadge(r().kind)}>
            <select class="select select-bordered w-full" value={kind()} onChange={(e) => setKind(e.currentTarget.value as ProductKind)}>
              <For each={PRODUCT_KINDS}>{(k) => <option value={k}>{k}</option>}</For>
            </select>
          </BladeField>
          <BladeField label="Type" mono value={() => r().component_type}>
            <TypeSelect value={componentType()} onChange={setComponentType} />
          </BladeField>
          <BladeField
            label="Icon"
            read={<IconPreview componentType={r().component_type} value={r().icon} />}
          >
            <IconOverrideField componentType={componentType()} value={icon()} onChange={setIcon} />
          </BladeField>
          <BladeField label="Vendor" mono value={() => r().vendor ?? ""}>
            <VendorSelect value={vendorId()} onChange={setVendorId} />
          </BladeField>
          <BladeField label="Driver" mono value={() => r().driver ?? ""}>
            <DriverSelect value={driverId()} onChange={setDriverId} />
          </BladeField>
          <ProductContractEditor productId={r().name} official={r().official} />
          <ProductContractEditor productId={r().name} official={r().official} lane="metric" />
        </div>
      )}
    </Show>
  );
}

// CreateProductForm: the label leads and the kebab name (the
// operator-facing address; the uuid is the database's to mint) derives from it
// until the operator edits it by hand (lib/entities). Kind defaults to device;
// component_type (the device-class genus, #614) is required, no default, so
// every product states its class explicitly rather than reading the silent
// default that let a mislabeled cloud service pass as correct forever
// (ADR-0086); vendor, driver, and icon override are optional.
export function CreateProductForm(p: { onCreated: (r: Product) => void }): JSX.Element {
  const qc = useQueryClient();
  const { display, setDisplay, name, setName, nameDerived } = createIdentity();
  const [kind, setKind] = createSignal<ProductKind>("device");
  const [componentType, setComponentType] = createSignal("");
  const [icon, setIcon] = createSignal("");
  const [vendorId, setVendorId] = createSignal("");
  const [driverId, setDriverId] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  useFormActions().bind({
    submitLabel: "Create product",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => !name().trim() || !display().trim() || !componentType(),
  });

  async function submit() {
    setBusy(true);
    setFormErr(null);
    try {
      const created = await createProduct({
        name: name().trim(),
        label: display().trim(),
        kind: kind(),
        component_type: componentType(),
        icon: icon() || undefined,
        vendor_id: vendorId() || undefined,
        driver_id: driverId() || undefined,
      });
      await qc.invalidateQueries({ queryKey: PRODUCTS_KEY });
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
        <input class="input input-bordered w-full" value={display()} placeholder="Crestron TSW-1070" onInput={(e) => setDisplay(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow bind="name" hint={nameDerived() ? "Derived from the label. Edit to set your own." : "Globally unique address, used by the API and CLI."}>
        <input class="input input-bordered w-full font-data" value={name()} placeholder="crestron-tsw-1070" onInput={(e) => setName(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Kind" hint="What class of thing the product is.">
        <select class="select select-bordered w-full" value={kind()} onChange={(e) => setKind(e.currentTarget.value as ProductKind)}>
          <For each={PRODUCT_KINDS}>{(k) => <option value={k}>{k}</option>}</For>
        </select>
      </FieldRow>
      <FieldRow label="Type" hint="The device-class genus this SKU is classified under. Required.">
        <TypeSelect value={componentType()} onChange={setComponentType} />
      </FieldRow>
      <FieldRow label="Icon" hint="A per-SKU override. Leave blank to use the type's icon.">
        <IconOverrideField componentType={componentType()} value={icon()} onChange={setIcon} />
      </FieldRow>
      <FieldRow label="Vendor" hint="Who makes it. Optional.">
        <VendorSelect value={vendorId()} onChange={setVendorId} />
      </FieldRow>
      <FieldRow label="Driver" hint="How its signals are collected. Optional.">
        <DriverSelect value={driverId()} onChange={setDriverId} />
      </FieldRow>
    </form>
  );
}

// VendorSelect: a vendor picker over the vendor registry, with a "None" option
// (a product need not name a vendor). Stores the vendor's name (the
// write path resolves either form).
function VendorSelect(p: { value: string; onChange: (v: string) => void }): JSX.Element {
  const vendors = useQuery(() => ({ queryKey: VENDORS_KEY, queryFn: listVendors }));
  const options = createMemo(() =>
    [...(vendors.data ?? [])].sort((a: Vendor, b: Vendor) => a.label.localeCompare(b.label)),
  );
  return (
    <select class="select select-bordered w-full" value={p.value} onChange={(e) => p.onChange(e.currentTarget.value)}>
      <option value="">None</option>
      <For each={options()}>{(v) => <option value={v.name}>{v.label}</option>}</For>
    </select>
  );
}

// DriverSelect: a driver picker over the driver registry, with a "None" option.
// Stores the driver's name (the write path resolves either form).
function DriverSelect(p: { value: string; onChange: (v: string) => void }): JSX.Element {
  const drivers = useQuery(() => ({ queryKey: DRIVERS_KEY, queryFn: listDrivers }));
  const options = createMemo(() =>
    [...(drivers.data ?? [])].sort((a: Driver, b: Driver) => a.label.localeCompare(b.label)),
  );
  return (
    <select class="select select-bordered w-full" value={p.value} onChange={(e) => p.onChange(e.currentTarget.value)}>
      <option value="">None</option>
      <For each={options()}>{(d) => <option value={d.name}>{d.label}</option>}</For>
    </select>
  );
}

// TypeSelect: the component_type classification picker, over the full seeded
// tree (ComponentTypeSelect), no "None" option since component_type is
// required (#614 makes the classification floor total).
function TypeSelect(p: { value: string; onChange: (v: string) => void }): JSX.Element {
  const types = useQuery(() => ({ queryKey: COMPONENT_TYPES_KEY, queryFn: listComponentTypes }));
  return <ComponentTypeSelect types={types.data ?? []} value={p.value} onChange={p.onChange} />;
}

// IconPreview: the read-mode glyph beside the icon key text, resolved the
// product's way (its own override, else the classified type's icon,
// #614/ADR-0085): "the icon lives on the type, products may override".
function IconPreview(p: { componentType: string; value?: string }): JSX.Element {
  const types = useQuery(() => ({ queryKey: COMPONENT_TYPES_KEY, queryFn: listComponentTypes }));
  const byName = createMemo(() => componentTypeByName(types.data ?? []));
  const resolved = createMemo(() => resolveProductIcon({ icon: p.value, component_type: p.componentType }, byName()));
  return (
    <span class="flex items-center gap-2">
      <span class="flex items-center justify-center rounded-box border border-base-300 p-1.5">
        <Dynamic component={resolveIcon(resolved())} size={14} />
      </span>
      <span class="font-data text-xs text-base-content/60">{p.value || resolved()}</span>
    </span>
  );
}

// IconOverrideField: the edit-mode counterpart, a bare text input (mirrors
// LocationTypes' Icon field). Its placeholder is the classified type's own
// icon, so a blank field visibly reads as "inherit the type's icon", not as
// an unset one; a live glyph preview stays on the read side (IconPreview),
// deliberately not duplicated here, since a wrapping element around the
// input would swallow its own accessible-label association (FieldRow labels
// the FIRST resolved element, and that must be the input itself).
function IconOverrideField(p: { componentType: string; value: string; onChange: (v: string) => void }): JSX.Element {
  const types = useQuery(() => ({ queryKey: COMPONENT_TYPES_KEY, queryFn: listComponentTypes }));
  const byName = createMemo(() => componentTypeByName(types.data ?? []));
  const typeIcon = createMemo(() => resolveProductIcon({ component_type: p.componentType }, byName()));
  return (
    <input
      class="input input-bordered w-full font-data"
      value={p.value}
      placeholder={typeIcon()}
      onInput={(e) => p.onChange(e.currentTarget.value)}
    />
  );
}

