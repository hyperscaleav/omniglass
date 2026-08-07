import { Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import BladeTitle from "../components/BladeTitle";
import { identityColumn } from "../components/IdentityCell";
import KVStacked from "../components/KVStacked";
import BladeField, { EMPTY_VALUE } from "../components/BladeField";
import FieldRow from "../components/FieldRow";
import { createIdentity } from "../lib/entities";
import { useFormActions } from "../lib/formactions";
import { Plus } from "../components/icons";
import {
  type Vendor,
  type VendorKind,
  VENDORS_KEY,
  listVendors,
  createVendor,
  updateVendor,
  deleteVendor,
} from "../lib/vendors";
import { useMe, can } from "../lib/auth";
import { registryLock } from "../lib/catalog";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Vendors: the vendor registry (the vendor picker on the product form), on the
// flat FlatList surface. A vendor is addressed by its kebab name (ADR-0062:
// the uuid is identity, the name is what an operator reads and types);
// official rows are read-only, same as the Types catalog's
// official rows: the Edit / Delete pair greys. Each vendor carries a kind
// (manufacturer/integrator/developer).

const VENDOR_KINDS: VendorKind[] = ["manufacturer", "integrator", "developer"];

// safeUrl allows only http(s) hrefs through to a live anchor. Website is
// operator-entered free text; without this check a stored javascript:/data:
// URL would execute on click (stored XSS). A value that fails the check
// still renders, as plain text (see VendorBladeBody), so nothing is silently
// dropped.
const safeUrl = (u?: string): string | undefined => {
  if (!u) return undefined;
  try {
    const p = new URL(u);
    return p.protocol === "http:" || p.protocol === "https:" ? p.toString() : undefined;
  } catch {
    return undefined;
  }
};

function officialBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

function kindBadge(kind: string): JSX.Element {
  return <span class="badge badge-ghost badge-sm">{kind}</span>;
}

const columns: FlatColumn<Vendor>[] = [
  identityColumn<Vendor>(),
  { key: "kind", label: "Kind", width: "130px", sortVal: (m) => m.kind, cell: (m) => kindBadge(m.kind) },
  { key: "icon", label: "Icon", width: "110px", cell: (m) => <span class="font-data text-xs text-base-content/60">{m.icon ?? "—"}</span> },
  { key: "official", label: "Origin", width: "100px", sortVal: (m) => String(m.official), cell: (m) => officialBadge(m.official) },
];

export default function Vendors() {
  const me = useMe();
  const makes = useQuery(() => ({ queryKey: VENDORS_KEY, queryFn: listVendors }));

  const rows = createMemo(() =>
    [...(makes.data ?? [])].sort((a, b) => a.display_name.localeCompare(b.display_name) || a.name.localeCompare(b.name)),
  );

  return (
    <FlatList<Vendor>
      config={{
        entity: { name: "vendor", plural: "vendors" },
        rows,
        loading: () => makes.isPending,
        error: () => makes.error,
        filterKeys: [
          { key: "name", type: "string", hint: "substring", get: (m) => `${m.name} ${m.display_name}`, values: () => [] },
          { key: "kind", type: "string", hint: "exact", get: (m) => m.kind, values: () => VENDOR_KINDS },
          { key: "official", type: "string", hint: "exact", get: (m) => (m.official ? "official" : "custom"), values: () => ["official", "custom"] },
        ],
        filterPlaceholder: "filter vendors by name…",
        columns,
        empty: "No vendors yet.",
        // Address a row by its kebab name: globally unique, and what the API
        // and CLI accept (the write paths resolve either form).
        rowId: (m) => m.name,
        blades: { registry: { vendor: vendorBlade }, rootKind: "vendor" },
        create: can(me.data, "vendor", "create")
          ? { label: "New vendor", can: () => can(me.data, "vendor", "create"), body: (ctx) => <CreateVendorForm onCreated={ctx.select} /> }
          : undefined,
      }}
    />
  );
}

// vendorBlade renders an id on the shared blade stack. An official row is
// read-only (the Edit / Delete pair greys); a custom row carries Edit +
// Delete.
export const vendorBlade: BladeDef = {
  Title: (p) => <VendorBladeTitle id={p.id} />,
  Body: (p) => <VendorBladeBody id={p.id} />,
};

function useVendorRow(id: string): () => Vendor | undefined {
  const makes = useQuery(() => ({ queryKey: VENDORS_KEY, queryFn: listVendors }));
  return () => (makes.data ?? []).find((m) => m.name === id);
}

function VendorBladeTitle(p: { id: string }): JSX.Element {
  return <BladeTitle row={useVendorRow(p.id)} fallback={p.id} />;
}

function VendorBladeBody(p: { id: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const row = useVendorRow(p.id);
  const [err, setErr] = createSignal<string | null>(null);
  const [displayName, setDisplayName] = createSignal("");
  const [kind, setKind] = createSignal<VendorKind>("manufacturer");
  const [icon, setIcon] = createSignal("");
  const [supportPhone, setSupportPhone] = createSignal("");
  const [website, setWebsite] = createSignal("");

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    const r = row();
    setDisplayName(r?.display_name ?? "");
    setKind(r?.kind ?? "manufacturer");
    setIcon(r?.icon ?? "");
    setSupportPhone(r?.support_phone ?? "");
    setWebsite(r?.website ?? "");
    setErr(null);
  }));

  async function removeVendor() {
    const r = row();
    if (!r) return;
    if (!confirm(`Delete vendor "${r.name}"?`)) return;
    setErr(null);
    try {
      await deleteVendor(r.name);
      blades.close();
      await qc.invalidateQueries({ queryKey: VENDORS_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const r = row();
    if (!r) return;
    setErr(null);
    try {
      await updateVendor(r.name, {
        display_name: displayName(),
        kind: kind(),
        icon: icon(),
        support_phone: supportPhone(),
        website: website(),
      });
      await qc.invalidateQueries({ queryKey: VENDORS_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  edit.bind({
    editable: () => !!row() && !row()!.official && can(me.data, "vendor", "update"),
    save,
    destructive: () =>
      row() && !row()!.official && can(me.data, "vendor", "delete")
        ? { label: "Delete", tone: "danger", onClick: removeVendor }
        : undefined,
    locked: () => registryLock(row(), me.data, "vendor"),
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Vendor not found.</p>}>
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
            bind="display_name"
            value={() => r().display_name ?? ""}
            draft={displayName}
            onInput={setDisplayName}
          />
          <BladeField label="Kind" read={kindBadge(r().kind)}>
            <select class="select select-bordered w-full" value={kind()} onChange={(e) => setKind(e.currentTarget.value as VendorKind)}>
              {VENDOR_KINDS.map((k) => <option value={k}>{k}</option>)}
            </select>
          </BladeField>
          <BladeField
            label="Icon"
            mono
            placeholder="crestron-logo"
            value={() => r().icon ?? ""}
            draft={icon}
            onInput={setIcon}
          />
          <BladeField
            label="Support phone"
            placeholder="+1 800 555 0100"
            value={() => r().support_phone ?? ""}
            draft={supportPhone}
            onInput={setSupportPhone}
          />
          <BladeField
            label="Website"
            placeholder="https://example.com"
            value={() => r().website ?? ""}
            draft={website}
            onInput={setWebsite}
            read={
              <Show when={safeUrl(r().website)} fallback={<span>{r().website || EMPTY_VALUE}</span>}>
                {(href) => (
                  <a class="link" href={href()} target="_blank" rel="noreferrer">{r().website}</a>
                )}
              </Show>
            }
          />
        </div>
      )}
    </Show>
  );
}

// CreateVendorForm: type the vendor as it is written on the box and the kebab
// name follows (the uuid is the database's to mint), then set the kind; icon,
// support phone, and website are optional.
export function CreateVendorForm(p: { onCreated: (v: Vendor) => void }): JSX.Element {
  const qc = useQueryClient();
  // Display name leads and the name follows it, stopping the moment the operator
  // edits the name by hand (lib/entities).
  const { display, setDisplay, name, setName, nameDerived } = createIdentity();
  const [kind, setKind] = createSignal<VendorKind>("manufacturer");
  const [icon, setIcon] = createSignal("");
  const [supportPhone, setSupportPhone] = createSignal("");
  const [website, setWebsite] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  useFormActions().bind({
    submitLabel: "Create vendor",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => !name().trim() || !display().trim(),
  });

  async function submit() {
    setBusy(true);
    setFormErr(null);
    try {
      const created = await createVendor({
        name: name().trim(),
        display_name: display().trim(),
        kind: kind(),
        icon: icon().trim() || undefined,
        support_phone: supportPhone().trim() || undefined,
        website: website().trim() || undefined,
      });
      await qc.invalidateQueries({ queryKey: VENDORS_KEY });
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
      <FieldRow bind="display_name" hint="What an operator reads, e.g. Crestron.">
        <input class="input input-bordered w-full" value={display()} placeholder="Crestron" onInput={(e) => setDisplay(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow
        bind="name"
        hint={nameDerived() ? "Derived from the display name. Edit to set your own." : "A kebab name, the address the API and CLI accept."}
      >
        <input class="input input-bordered w-full font-data" value={name()} placeholder="crestron" onInput={(e) => setName(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Kind" hint="What role the vendor plays.">
        <select class="select select-bordered w-full" value={kind()} onChange={(e) => setKind(e.currentTarget.value as VendorKind)}>
          {VENDOR_KINDS.map((k) => <option value={k}>{k}</option>)}
        </select>
      </FieldRow>
      <FieldRow label="Icon" hint="A glyph key, e.g. crestron-logo. Optional.">
        <input class="input input-bordered w-full font-data" value={icon()} placeholder="crestron-logo" onInput={(e) => setIcon(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Support phone" hint="Optional.">
        <input class="input input-bordered w-full" value={supportPhone()} placeholder="+1 800 555 0100" onInput={(e) => setSupportPhone(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Website" hint="Optional.">
        <input class="input input-bordered w-full" value={website()} placeholder="https://example.com" onInput={(e) => setWebsite(e.currentTarget.value)} />
      </FieldRow>
    </form>
  );
}
