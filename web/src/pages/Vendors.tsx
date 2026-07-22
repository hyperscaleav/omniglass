import { Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import KVStacked from "../components/KVStacked";
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
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Vendors: the vendor registry (the vendor picker on the product form), on the
// flat FlatList surface. A vendor is addressed by its id (a kebab id,
// create-only); official (seed-owned) rows are read-only, same as the Types
// catalog's official rows: no Edit pencil, no Delete. Each vendor carries a
// kind (manufacturer/integrator/developer).

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
  { key: "id", label: "Id", sortVal: (m) => m.id, cell: (m) => <span class="font-data font-semibold">{m.id}</span> },
  { key: "display_name", label: "Display name", sortVal: (m) => m.display_name, cell: (m) => <span>{m.display_name}</span> },
  { key: "kind", label: "Kind", width: "130px", sortVal: (m) => m.kind, cell: (m) => kindBadge(m.kind) },
  { key: "icon", label: "Icon", width: "110px", cell: (m) => <span class="font-data text-xs text-base-content/60">{m.icon ?? "—"}</span> },
  { key: "official", label: "Origin", width: "100px", sortVal: (m) => String(m.official), cell: (m) => officialBadge(m.official) },
];

export default function Vendors() {
  const me = useMe();
  const makes = useQuery(() => ({ queryKey: VENDORS_KEY, queryFn: listVendors }));

  const rows = createMemo(() =>
    [...(makes.data ?? [])].sort((a, b) => a.display_name.localeCompare(b.display_name) || a.id.localeCompare(b.id)),
  );

  return (
    <FlatList<Vendor>
      config={{
        entity: { name: "vendor", plural: "vendors" },
        rows,
        loading: () => makes.isPending,
        error: () => makes.error,
        filterKeys: [
          { key: "name", type: "string", hint: "substring", get: (m) => `${m.id} ${m.display_name}`, values: () => [] },
          { key: "kind", type: "string", hint: "exact", get: (m) => m.kind, values: () => VENDOR_KINDS },
          { key: "official", type: "string", hint: "exact", get: (m) => (m.official ? "official" : "custom"), values: () => ["official", "custom"] },
        ],
        filterPlaceholder: "filter vendors by id, name…",
        columns,
        empty: "No vendors yet.",
        // Address a row by id: the write paths key on id, and it is globally unique.
        rowId: (m) => m.id,
        blades: { registry: { vendor: vendorBlade }, rootKind: "vendor" },
        create: can(me.data, "vendor", "create")
          ? { label: "New vendor", can: () => can(me.data, "vendor", "create"), body: (ctx) => <CreateVendorForm onCreated={ctx.close} /> }
          : undefined,
      }}
    />
  );
}

// vendorBlade renders an id on the shared blade stack. An official row is
// read-only (no pencil, no destructive action); a custom row carries Edit +
// Delete.
export const vendorBlade: BladeDef = {
  Title: (p) => <VendorBladeTitle id={p.id} />,
  Body: (p) => <VendorBladeBody id={p.id} />,
};

function useVendorRow(id: string): () => Vendor | undefined {
  const makes = useQuery(() => ({ queryKey: VENDORS_KEY, queryFn: listVendors }));
  return () => (makes.data ?? []).find((m) => m.id === id);
}

function VendorBladeTitle(p: { id: string }): JSX.Element {
  const row = useVendorRow(p.id);
  return <span class="font-data">{row()?.id ?? p.id}</span>;
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
    if (!confirm(`Delete vendor "${r.id}"?`)) return;
    setErr(null);
    try {
      await deleteVendor(r.id);
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
      await updateVendor(r.id, {
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
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Vendor not found.</p>}>
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
              <select class="select select-bordered w-full" value={kind()} onChange={(e) => setKind(e.currentTarget.value as VendorKind)}>
                {VENDOR_KINDS.map((k) => <option value={k}>{k}</option>)}
              </select>
            </Show>
          </div>
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Icon</span>
            <Show
              when={edit.editing()}
              fallback={<div class="input input-bordered flex items-center text-sm font-data">{r().icon || "—"}</div>}
            >
              <input class="input input-bordered w-full font-data" placeholder="crestron-logo" value={icon()} onInput={(e) => setIcon(e.currentTarget.value)} />
            </Show>
          </div>
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Support phone</span>
            <Show
              when={edit.editing()}
              fallback={<div class="input input-bordered flex items-center text-sm">{r().support_phone || "—"}</div>}
            >
              <input class="input input-bordered w-full" placeholder="+1 800 555 0100" value={supportPhone()} onInput={(e) => setSupportPhone(e.currentTarget.value)} />
            </Show>
          </div>
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Website</span>
            <Show
              when={edit.editing()}
              fallback={
                <Show when={r().website} fallback={<div class="input input-bordered flex items-center text-sm">—</div>}>
                  <Show
                    when={safeUrl(r().website)}
                    fallback={<div class="input input-bordered flex items-center text-sm">{r().website}</div>}
                  >
                    {(href) => (
                      <a class="link input input-bordered flex items-center text-sm" href={href()} target="_blank" rel="noreferrer">
                        {r().website}
                      </a>
                    )}
                  </Show>
                </Show>
              }
            >
              <input class="input input-bordered w-full" placeholder="https://example.com" value={website()} onInput={(e) => setWebsite(e.currentTarget.value)} />
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

// CreateVendorForm: name the id (a kebab id, immutable after creation), set the
// display name and kind; icon, support phone, and website are optional.
export function CreateVendorForm(p: { onCreated: (id: string) => void }): JSX.Element {
  const qc = useQueryClient();
  const [id, setId] = createSignal("");
  const [displayName, setDisplayName] = createSignal("");
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
    disabled: () => !id().trim() || !displayName().trim(),
  });

  async function submit() {
    setBusy(true);
    setFormErr(null);
    try {
      await createVendor({
        id: id().trim(),
        display_name: displayName().trim(),
        kind: kind(),
        icon: icon().trim() || undefined,
        support_phone: supportPhone().trim() || undefined,
        website: website().trim() || undefined,
      });
      await qc.invalidateQueries({ queryKey: VENDORS_KEY });
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
      <Field label="Id" hint="A kebab id, e.g. crestron.">
        <input class="input input-bordered w-full font-data" value={id()} placeholder="crestron" onInput={(e) => setId(e.currentTarget.value)} />
      </Field>
      <Field label="Display name">
        <input class="input input-bordered w-full" value={displayName()} placeholder="Crestron" onInput={(e) => setDisplayName(e.currentTarget.value)} />
      </Field>
      <Field label="Kind" hint="What role the vendor plays.">
        <select class="select select-bordered w-full" value={kind()} onChange={(e) => setKind(e.currentTarget.value as VendorKind)}>
          {VENDOR_KINDS.map((k) => <option value={k}>{k}</option>)}
        </select>
      </Field>
      <Field label="Icon" hint="A glyph key, e.g. crestron-logo. Optional.">
        <input class="input input-bordered w-full font-data" value={icon()} placeholder="crestron-logo" onInput={(e) => setIcon(e.currentTarget.value)} />
      </Field>
      <Field label="Support phone" hint="Optional.">
        <input class="input input-bordered w-full" value={supportPhone()} placeholder="+1 800 555 0100" onInput={(e) => setSupportPhone(e.currentTarget.value)} />
      </Field>
      <Field label="Website" hint="Optional.">
        <input class="input input-bordered w-full" value={website()} placeholder="https://example.com" onInput={(e) => setWebsite(e.currentTarget.value)} />
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
