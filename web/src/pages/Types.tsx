import { For, Show, createEffect, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import Button from "../components/Button";
import { DrawerFooter } from "../components/Drawer";
import { Plus } from "../components/icons";
import {
  type TypeKind,
  type TypeRow,
  TYPE_KINDS,
  TYPES_KEY,
  listTypes,
  createType,
  updateType,
  deleteType,
} from "../lib/types";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Types: the classifier registries (location, system, component, secret), one
// per tab. Each tab is that registry's own directory over the FlatList surface;
// a row is addressed by kind + id (the write paths key on id within a kind, not
// globally). secret_type and any official (seed-owned) row are read-only this
// slice; the writable rows are custom location/system/component entries.

const KIND_LABEL: Record<TypeKind, string> = {
  location: "Location",
  system: "System",
  component: "Component",
  secret: "Secret",
};

function kindBadge(kind: TypeKind): JSX.Element {
  return <span class="badge badge-ghost badge-sm font-data">{kind}</span>;
}

function officialBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

// Columns for the active kind: every kind shows id, display name, and origin;
// the writable kinds add rank; location alone adds its icon glyph. There is no
// Kind column because the tab already names the kind.
function columnsFor(kind: TypeKind): FlatColumn<TypeRow>[] {
  const cols: FlatColumn<TypeRow>[] = [
    { key: "id", label: "Id", sortVal: (r) => r.id, cell: (r) => <span class="font-data font-semibold">{r.id}</span> },
    { key: "display_name", label: "Display name", sortVal: (r) => r.display_name, cell: (r) => <span>{r.display_name}</span> },
  ];
  if (kind !== "secret") {
    cols.push({ key: "rank", label: "Rank", width: "80px", sortVal: (r) => r.rank ?? Number.MAX_SAFE_INTEGER, cell: (r) => <span class="text-base-content/70">{r.rank ?? "—"}</span> });
  }
  if (kind === "location") {
    cols.push({ key: "icon", label: "Icon", width: "110px", cell: (r) => <span class="font-data text-xs text-base-content/60">{r.icon ?? "—"}</span> });
  }
  cols.push({ key: "official", label: "Origin", width: "100px", sortVal: (r) => String(r.official), cell: (r) => officialBadge(r.official) });
  return cols;
}

export default function Types() {
  const me = useMe();
  const types = useQuery(() => ({ queryKey: TYPES_KEY, queryFn: listTypes }));
  const [kind, setKind] = createSignal<TypeKind>("location");

  // Rows for the active kind, ranked ascending then by id (secret has no rank,
  // so it falls straight through to id).
  const rowsFor = (k: TypeKind) =>
    (types.data ?? [])
      .filter((r) => r.kind === k)
      .sort((a, b) => {
        const ra = a.rank ?? Number.MAX_SAFE_INTEGER;
        const rb = b.rank ?? Number.MAX_SAFE_INTEGER;
        if (ra !== rb) return ra - rb;
        return a.id.localeCompare(b.id);
      });

  return (
    <div class="flex min-h-full flex-col gap-4">
      <div role="tablist" class="tabs tabs-box w-fit">
        <For each={TYPE_KINDS}>
          {(k) => (
            <button
              role="tab"
              class="tab"
              classList={{ "tab-active": kind() === k }}
              onClick={() => setKind(k)}
            >
              {KIND_LABEL[k]}
            </button>
          )}
        </For>
      </div>
      {/* Keyed on the active kind so the FlatList rebuilds with that kind's
          static config (columns, create, placeholder); the row list itself stays
          a live accessor over the shared listTypes query. */}
      <Show when={kind()} keyed>
        {(k) => {
          const label = KIND_LABEL[k].toLowerCase();
          const canCreate = () => k !== "secret" && can(me.data, "type", "create");
          return (
            <FlatList<TypeRow>
              config={{
                entity: { name: "type", plural: "types" },
                rows: () => rowsFor(k),
                loading: () => types.isPending,
                error: () => types.error,
                filterKeys: [
                  { key: "name", type: "string", hint: "substring", get: (r) => `${r.id} ${r.display_name}`, values: () => [] },
                  { key: "official", type: "string", hint: "exact", get: (r) => (r.official ? "official" : "custom"), values: () => ["official", "custom"] },
                ],
                filterPlaceholder: `filter ${label} types by id, name…`,
                columns: columnsFor(k),
                empty: `No ${label} types.`,
                // Address a row by kind + id: the registries are per-kind, and an
                // id is unique only within its own kind.
                rowId: (r) => `${r.kind}:${r.id}`,
                blades: { registry: { type: typeBlade }, rootKind: "type" },
                create: canCreate()
                  ? { label: "New type", can: canCreate, body: (ctx) => <CreateTypeForm kind={k} onCreated={ctx.close} /> }
                  : undefined,
              }}
            />
          );
        }}
      </Show>
    </div>
  );
}

// typeBlade renders a kind:id row on the shared blade stack. Secret rows and
// official rows are read-only (no pencil, no destructive action); a custom
// location/system/component row carries Edit + Delete.
export const typeBlade: BladeDef = {
  Title: (p) => <TypeBladeTitle id={p.id} />,
  Body: (p) => <TypeBladeBody id={p.id} />,
};

// The blade id is "<kind>:<id>"; split on the FIRST colon (ids are kebab, no
// colons of their own) and look the row up from the cached listTypes query.
function splitBladeId(id: string): { kind: TypeKind; id: string } {
  const i = id.indexOf(":");
  return i < 0 ? { kind: id as TypeKind, id: "" } : { kind: id.slice(0, i) as TypeKind, id: id.slice(i + 1) };
}

function useTypeRow(id: string): () => TypeRow | undefined {
  const types = useQuery(() => ({ queryKey: TYPES_KEY, queryFn: listTypes }));
  const { kind, id: rowId } = splitBladeId(id);
  return () => (types.data ?? []).find((r) => r.kind === kind && r.id === rowId);
}

function TypeBladeTitle(p: { id: string }): JSX.Element {
  const row = useTypeRow(p.id);
  return <span class="font-data">{row()?.id ?? splitBladeId(p.id).id}</span>;
}

function TypeBladeBody(p: { id: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const row = useTypeRow(p.id);
  const [err, setErr] = createSignal<string | null>(null);
  const [displayName, setDisplayName] = createSignal("");
  const [rank, setRank] = createSignal(0);
  const [icon, setIcon] = createSignal("");

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    const r = row();
    setDisplayName(r?.display_name ?? "");
    setRank(r?.rank ?? 0);
    setIcon(r?.icon ?? "");
    setErr(null);
  }));

  async function removeType() {
    const r = row();
    if (!r) return;
    if (!confirm(`Delete ${r.kind} type "${r.id}"?`)) return;
    setErr(null);
    try {
      await deleteType(r.kind, r.id);
      blades.close();
      await qc.invalidateQueries({ queryKey: TYPES_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const r = row();
    if (!r) return;
    setErr(null);
    try {
      await updateType(r.kind, r.id, {
        display_name: displayName(),
        rank: rank(),
        ...(r.kind === "location" ? { icon: icon() } : {}),
      });
      await qc.invalidateQueries({ queryKey: TYPES_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  edit.bind({
    editable: () => !!row() && row()!.kind !== "secret" && !row()!.official && can(me.data, "type", "update"),
    save,
    destructive: () =>
      row() && row()!.kind !== "secret" && !row()!.official && can(me.data, "type", "delete")
        ? { label: "Delete", tone: "danger", onClick: removeType }
        : undefined,
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Type not found.</p>}>
      {(r) => (
        <div class="flex flex-col gap-4">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <Fact label="Kind">{kindBadge(r().kind)}</Fact>
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
          <Show when={r().kind !== "secret"}>
            <div class="flex flex-col gap-1.5">
              <span class="eyebrow">Rank</span>
              <Show
                when={edit.editing()}
                fallback={<div class="input input-bordered flex items-center text-sm">{r().rank ?? "—"}</div>}
              >
                <input
                  type="number"
                  class="input input-bordered w-full font-data"
                  value={rank()}
                  onInput={(e) => setRank(Number(e.currentTarget.value))}
                />
              </Show>
            </div>
          </Show>
          <Show when={r().kind === "location"}>
            <div class="flex flex-col gap-1.5">
              <span class="eyebrow">Icon</span>
              <Show
                when={edit.editing()}
                fallback={<div class="input input-bordered flex items-center text-sm font-data">{r().icon ?? "map-pin"}</div>}
              >
                <input class="input input-bordered w-full font-data" placeholder="map-pin" value={icon()} onInput={(e) => setIcon(e.currentTarget.value)} />
              </Show>
            </div>
          </Show>
          <Show when={r().kind === "secret"}>
            <div class="flex flex-col gap-1.5">
              <span class="eyebrow">Fields</span>
              <div class="flex flex-col gap-2 rounded-box border border-base-300 p-2.5">
                <For each={r().fields} fallback={<span class="text-[11px] text-base-content/40">No fields declared.</span>}>
                  {(f) => (
                    <div class="flex items-center justify-between gap-2 text-sm">
                      <span class="font-data">{f.name}</span>
                      <span class="flex items-center gap-1.5 text-xs text-base-content/60">
                        <span class="badge badge-ghost badge-sm font-data">{f.type}</span>
                        <Show when={f.secret}><span class="badge badge-ghost badge-sm">secret</span></Show>
                        <span class="text-base-content/40">{f.origin}</span>
                      </span>
                    </div>
                  )}
                </For>
              </div>
              <span class="text-[11px] text-base-content/40">Secret types are read-only here; editing the fields schema is a follow-up.</span>
            </div>
          </Show>
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

// CreateTypeForm: name the id and set the display name + rank for a new custom
// type of the active kind (the tab decides the kind; secret_type has no write
// routes this slice, so it never opens this form). A location type also gets an
// icon glyph key.
export function CreateTypeForm(p: { kind: TypeKind; onCreated: (id: string) => void }): JSX.Element {
  const qc = useQueryClient();
  const [id, setId] = createSignal("");
  const [displayName, setDisplayName] = createSignal("");
  const [rank, setRank] = createSignal(0);
  const [icon, setIcon] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  async function submit(e: Event) {
    e.preventDefault();
    setBusy(true);
    setFormErr(null);
    try {
      await createType(p.kind, {
        id: id().trim(),
        display_name: displayName().trim(),
        rank: rank(),
        ...(p.kind === "location" ? { icon: icon().trim() || "map-pin" } : {}),
      });
      await qc.invalidateQueries({ queryKey: TYPES_KEY });
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
      <div class="flex items-center gap-2 text-sm text-base-content/70">
        <span class="eyebrow">Kind</span>
        {kindBadge(p.kind)}
      </div>
      <Field label="Id" hint="A kebab id, e.g. wing.">
        <input class="input input-bordered w-full font-data" value={id()} placeholder="wing" onInput={(e) => setId(e.currentTarget.value)} />
      </Field>
      <Field label="Display name">
        <input class="input input-bordered w-full" value={displayName()} placeholder="Wing" onInput={(e) => setDisplayName(e.currentTarget.value)} />
      </Field>
      <Field label="Rank" hint="Sort order within the kind; lower ranks first.">
        <input
          type="number"
          class="input input-bordered w-full font-data"
          value={rank()}
          onInput={(e) => setRank(Number(e.currentTarget.value))}
        />
      </Field>
      <Show when={p.kind === "location"}>
        <Field label="Icon" hint="A glyph key, e.g. map-pin (the default).">
          <input class="input input-bordered w-full font-data" value={icon()} placeholder="map-pin" onInput={(e) => setIcon(e.currentTarget.value)} />
        </Field>
      </Show>
      <DrawerFooter>
        <Button type="submit" intent="action" icon={Plus} disabled={busy() || !id().trim() || !displayName().trim()}>Create type</Button>
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
