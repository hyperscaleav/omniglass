import { Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import Button from "../components/Button";
import { DrawerFooter } from "../components/Drawer";
import { Plus } from "../components/icons";
import {
  type ComponentMake,
  COMPONENT_MAKES_KEY,
  listMakes,
  createMake,
  updateMake,
  deleteMake,
} from "../lib/component_makes";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// ComponentMakes: the manufacturer registry (the make picker on the
// component_model form), on the flat FlatList surface. A make is addressed by
// its id (a kebab id, create-only); official (seed-owned) rows are read-only,
// same as the Types catalog's official rows: no Edit pencil, no Delete.

function officialBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

const columns: FlatColumn<ComponentMake>[] = [
  { key: "id", label: "Id", sortVal: (m) => m.id, cell: (m) => <span class="font-data font-semibold">{m.id}</span> },
  { key: "display_name", label: "Display name", sortVal: (m) => m.display_name, cell: (m) => <span>{m.display_name}</span> },
  { key: "icon", label: "Icon", width: "110px", cell: (m) => <span class="font-data text-xs text-base-content/60">{m.icon ?? "—"}</span> },
  { key: "official", label: "Origin", width: "100px", sortVal: (m) => String(m.official), cell: (m) => officialBadge(m.official) },
];

export default function ComponentMakes() {
  const me = useMe();
  const makes = useQuery(() => ({ queryKey: COMPONENT_MAKES_KEY, queryFn: listMakes }));

  const rows = createMemo(() =>
    [...(makes.data ?? [])].sort((a, b) => a.display_name.localeCompare(b.display_name) || a.id.localeCompare(b.id)),
  );

  return (
    <FlatList<ComponentMake>
      config={{
        entity: { name: "make", plural: "makes" },
        rows,
        loading: () => makes.isPending,
        error: () => makes.error,
        filterKeys: [
          { key: "name", type: "string", hint: "substring", get: (m) => `${m.id} ${m.display_name}`, values: () => [] },
          { key: "official", type: "string", hint: "exact", get: (m) => (m.official ? "official" : "custom"), values: () => ["official", "custom"] },
        ],
        filterPlaceholder: "filter makes by id, name…",
        columns,
        empty: "No component makes yet.",
        // Address a row by id: the write paths key on id, and it is globally unique.
        rowId: (m) => m.id,
        blades: { registry: { make: makeBlade }, rootKind: "make" },
        create: can(me.data, "make", "create")
          ? { label: "New make", can: () => can(me.data, "make", "create"), body: (ctx) => <CreateMakeForm onCreated={ctx.close} /> }
          : undefined,
      }}
    />
  );
}

// makeBlade renders an id on the shared blade stack. An official row is
// read-only (no pencil, no destructive action); a custom row carries Edit +
// Delete.
export const makeBlade: BladeDef = {
  Title: (p) => <MakeBladeTitle id={p.id} />,
  Body: (p) => <MakeBladeBody id={p.id} />,
};

function useMakeRow(id: string): () => ComponentMake | undefined {
  const makes = useQuery(() => ({ queryKey: COMPONENT_MAKES_KEY, queryFn: listMakes }));
  return () => (makes.data ?? []).find((m) => m.id === id);
}

function MakeBladeTitle(p: { id: string }): JSX.Element {
  const row = useMakeRow(p.id);
  return <span class="font-data">{row()?.id ?? p.id}</span>;
}

function MakeBladeBody(p: { id: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const row = useMakeRow(p.id);
  const [err, setErr] = createSignal<string | null>(null);
  const [displayName, setDisplayName] = createSignal("");
  const [icon, setIcon] = createSignal("");
  const [supportPhone, setSupportPhone] = createSignal("");
  const [website, setWebsite] = createSignal("");

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    const r = row();
    setDisplayName(r?.display_name ?? "");
    setIcon(r?.icon ?? "");
    setSupportPhone(r?.support_phone ?? "");
    setWebsite(r?.website ?? "");
    setErr(null);
  }));

  async function removeMake() {
    const r = row();
    if (!r) return;
    if (!confirm(`Delete make "${r.id}"?`)) return;
    setErr(null);
    try {
      await deleteMake(r.id);
      blades.close();
      await qc.invalidateQueries({ queryKey: COMPONENT_MAKES_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const r = row();
    if (!r) return;
    setErr(null);
    try {
      await updateMake(r.id, {
        display_name: displayName(),
        icon: icon(),
        support_phone: supportPhone(),
        website: website(),
      });
      await qc.invalidateQueries({ queryKey: COMPONENT_MAKES_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  edit.bind({
    editable: () => !!row() && !row()!.official && can(me.data, "make", "update"),
    save,
    destructive: () =>
      row() && !row()!.official && can(me.data, "make", "delete")
        ? { label: "Delete", tone: "danger", onClick: removeMake }
        : undefined,
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Make not found.</p>}>
      {(r) => (
        <div class="flex flex-col gap-4">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <Fact label="Id"><span class="font-data">{r().id}</span></Fact>
            <Fact label="Origin">{officialBadge(r().official)}</Fact>
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
                  <a class="link input input-bordered flex items-center text-sm" href={r().website} target="_blank" rel="noreferrer">{r().website}</a>
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

function Fact(p: { label: string; children: JSX.Element }): JSX.Element {
  return (
    <div class="flex flex-col gap-0.5">
      <span class="text-[11px] uppercase tracking-wide text-base-content/40">{p.label}</span>
      <span>{p.children}</span>
    </div>
  );
}

// CreateMakeForm: name the id (a kebab id, immutable after creation) and set the
// display name; icon, support phone, and website are optional.
export function CreateMakeForm(p: { onCreated: (id: string) => void }): JSX.Element {
  const qc = useQueryClient();
  const [id, setId] = createSignal("");
  const [displayName, setDisplayName] = createSignal("");
  const [icon, setIcon] = createSignal("");
  const [supportPhone, setSupportPhone] = createSignal("");
  const [website, setWebsite] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  async function submit(e: Event) {
    e.preventDefault();
    setBusy(true);
    setFormErr(null);
    try {
      await createMake({
        id: id().trim(),
        display_name: displayName().trim(),
        icon: icon().trim() || undefined,
        support_phone: supportPhone().trim() || undefined,
        website: website().trim() || undefined,
      });
      await qc.invalidateQueries({ queryKey: COMPONENT_MAKES_KEY });
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
      <Field label="Id" hint="A kebab id, e.g. crestron.">
        <input class="input input-bordered w-full font-data" value={id()} placeholder="crestron" onInput={(e) => setId(e.currentTarget.value)} />
      </Field>
      <Field label="Display name">
        <input class="input input-bordered w-full" value={displayName()} placeholder="Crestron" onInput={(e) => setDisplayName(e.currentTarget.value)} />
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
      <DrawerFooter>
        <Button type="submit" intent="action" icon={Plus} disabled={busy() || !id().trim() || !displayName().trim()}>Create make</Button>
      </DrawerFooter>
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
