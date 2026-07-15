import { For, Show, createEffect, createMemo, createResource, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import Button from "../components/Button";
import { DrawerFooter } from "../components/Drawer";
import { Plus } from "../components/icons";
import {
  type ComponentModel,
  COMPONENT_MODELS_KEY,
  listModels,
  createModel,
  updateModel,
  deleteModel,
} from "../lib/component_models";
import { type ComponentMake, COMPONENT_MAKES_KEY, listMakes } from "../lib/component_makes";
import { createFile, downloadFile, dataUrlToBase64 } from "../lib/files";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// ComponentModels: the product catalog (the primary catalog browse surface), on
// the flat FlatList surface. A model is addressed by its id (a kebab id,
// create-only); official (seed-owned) rows are read-only, same invariant as
// the Makes and Types catalogs: no Edit pencil, no Delete. make_id is
// create-only (not patchable): the make picker only appears on create, the
// blade shows it as a resolved, read-only fact. Front/back product photos ride
// as file ids: an upload goes through the files API (createFile), and the
// blade previews them by downloading the referenced file to a data URL.

function officialBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

// toDateInput/fromDateInput bridge a native <input type="date"> (a bare
// YYYY-MM-DD string) and the API's date-time fields (an RFC3339 string): the
// day is stored at midnight UTC, and only the day is ever shown or edited.
function toDateInput(iso?: string): string {
  return iso ? iso.slice(0, 10) : "";
}
function fromDateInput(v: string): string | undefined {
  return v.trim() ? `${v.trim()}T00:00:00Z` : undefined;
}
function fmtDateOnly(iso?: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  return isNaN(d.getTime()) ? iso : d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

// readAsDataUrl wraps FileReader in a promise so an upload handler can await
// the picked bytes before base64-encoding them for the files API.
function readAsDataUrl(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result ?? ""));
    reader.onerror = () => reject(new Error("Could not read that file."));
    reader.readAsDataURL(file);
  });
}

// useMakesMap resolves the make registry once, shared by the directory's Make
// column, the make filter facet, the create form's picker, and the blade's
// read-only fact, so they all agree on the same id -> display-name mapping.
function useMakesMap(): () => Map<string, ComponentMake> {
  const makes = useQuery(() => ({ queryKey: COMPONENT_MAKES_KEY, queryFn: listMakes }));
  return () => new Map((makes.data ?? []).map((m) => [m.id, m] as const));
}

export default function ComponentModels() {
  const me = useMe();
  const models = useQuery(() => ({ queryKey: COMPONENT_MODELS_KEY, queryFn: listModels }));
  const makesById = useMakesMap();
  const makeName = (id: string): string => makesById().get(id)?.display_name ?? id;

  const rows = createMemo(() =>
    [...(models.data ?? [])].sort((a, b) => a.display_name.localeCompare(b.display_name) || a.id.localeCompare(b.id)),
  );

  const columns: FlatColumn<ComponentModel>[] = [
    { key: "id", label: "Id", sortVal: (m) => m.id, cell: (m) => <span class="font-data font-semibold">{m.id}</span> },
    { key: "display_name", label: "Display name", sortVal: (m) => m.display_name, cell: (m) => <span>{m.display_name}</span> },
    { key: "make", label: "Make", sortVal: (m) => makeName(m.make_id), cell: (m) => <span>{makeName(m.make_id)}</span> },
    { key: "model_number", label: "Model#", width: "160px", sortVal: (m) => m.model_number, cell: (m) => <span class="font-data text-xs">{m.model_number}</span> },
    { key: "official", label: "Origin", width: "100px", sortVal: (m) => String(m.official), cell: (m) => officialBadge(m.official) },
  ];

  return (
    <FlatList<ComponentModel>
      config={{
        entity: { name: "model", plural: "models" },
        rows,
        loading: () => models.isPending,
        error: () => models.error,
        // "name" is a substring catch-all (id, display name, model#, family); "make"
        // facets on the stable make_id (not the display name, which is derived and
        // could collide), with valueLabel resolving the suggestion to a readable name.
        filterKeys: [
          { key: "name", type: "string", hint: "substring", get: (m) => `${m.id} ${m.display_name} ${m.model_number} ${m.family ?? ""}`, values: () => [] },
          { key: "make", type: "string", hint: "exact", get: (m) => m.make_id, values: (rs) => [...new Set(rs.map((r) => r.make_id))].sort(), valueLabel: (id) => makeName(id) },
          { key: "official", type: "string", hint: "exact", get: (m) => (m.official ? "official" : "custom"), values: () => ["official", "custom"] },
        ],
        filterPlaceholder: "filter models by id, name, make…",
        columns,
        empty: "No component models yet.",
        // Address a row by id: the write paths key on id, and it is globally unique.
        rowId: (m) => m.id,
        blades: { registry: { model: modelBlade }, rootKind: "model" },
        create: can(me.data, "model", "create")
          ? { label: "New model", can: () => can(me.data, "model", "create"), body: (ctx) => <CreateModelForm onCreated={ctx.close} /> }
          : undefined,
      }}
    />
  );
}

// modelBlade renders an id on the shared blade stack. An official row is
// read-only (no pencil, no destructive action); a custom row carries Edit +
// Delete.
export const modelBlade: BladeDef = {
  Title: (p) => <ModelBladeTitle id={p.id} />,
  Body: (p) => <ModelBladeBody id={p.id} />,
};

function useModelRow(id: string): () => ComponentModel | undefined {
  const models = useQuery(() => ({ queryKey: COMPONENT_MODELS_KEY, queryFn: listModels }));
  return () => (models.data ?? []).find((m) => m.id === id);
}

function ModelBladeTitle(p: { id: string }): JSX.Element {
  const row = useModelRow(p.id);
  return <span class="font-data">{row()?.id ?? p.id}</span>;
}

function ModelBladeBody(p: { id: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const row = useModelRow(p.id);
  const makesById = useMakesMap();
  const [err, setErr] = createSignal<string | null>(null);
  const [displayName, setDisplayName] = createSignal("");
  const [modelNumber, setModelNumber] = createSignal("");
  const [family, setFamily] = createSignal("");
  const [releasedAt, setReleasedAt] = createSignal("");
  const [eosAt, setEosAt] = createSignal("");
  const [eolAt, setEolAt] = createSignal("");
  const [frontImageId, setFrontImageId] = createSignal<string | undefined>();
  const [backImageId, setBackImageId] = createSignal<string | undefined>();

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    const r = row();
    setDisplayName(r?.display_name ?? "");
    setModelNumber(r?.model_number ?? "");
    setFamily(r?.family ?? "");
    setReleasedAt(toDateInput(r?.released_at));
    setEosAt(toDateInput(r?.eos_at));
    setEolAt(toDateInput(r?.eol_at));
    setFrontImageId(r?.front_image_id);
    setBackImageId(r?.back_image_id);
    setErr(null);
  }));

  async function removeModel() {
    const r = row();
    if (!r) return;
    if (!confirm(`Delete model "${r.id}"?`)) return;
    setErr(null);
    try {
      await deleteModel(r.id);
      blades.close();
      await qc.invalidateQueries({ queryKey: COMPONENT_MODELS_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const r = row();
    if (!r) return;
    setErr(null);
    try {
      await updateModel(r.id, {
        display_name: displayName(),
        model_number: modelNumber(),
        // family is stored NOT NULL DEFAULT '', so send the raw trimmed
        // string (empty string included): the server's coalesce($n, family)
        // treats a present-but-empty string as a real value, clearing it.
        // family().trim() || undefined would silently no-op an emptied field.
        family: family().trim(),
        // TODO(#260): released_at/eos_at/eol_at/front_image_id/back_image_id
        // are set/replace-only; clearing them needs explicit-null patch
        // semantics (coalesce keeps the old value when the field is omitted,
        // and these are nullable columns where undefined is the only way to
        // "not send" from this client today).
        released_at: fromDateInput(releasedAt()),
        eos_at: fromDateInput(eosAt()),
        eol_at: fromDateInput(eolAt()),
        front_image_id: frontImageId(),
        back_image_id: backImageId(),
      });
      await qc.invalidateQueries({ queryKey: COMPONENT_MODELS_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  edit.bind({
    editable: () => !!row() && !row()!.official && can(me.data, "model", "update"),
    // model_number is required server-side (a nonempty CHECK constraint plus
    // Huma's minLength:1 on the update body): gate Save so clearing it can't
    // be committed, mirroring the create form's disabled-state guard.
    valid: () => modelNumber().trim() !== "",
    save,
    destructive: () =>
      row() && !row()!.official && can(me.data, "model", "delete")
        ? { label: "Delete", tone: "danger", onClick: removeModel }
        : undefined,
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Model not found.</p>}>
      {(r) => (
        <div class="flex flex-col gap-4">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <Fact label="Id"><span class="font-data">{r().id}</span></Fact>
            <Fact label="Origin">{officialBadge(r().official)}</Fact>
            <Fact label="Make"><span>{makesById().get(r().make_id)?.display_name ?? r().make_id}</span></Fact>
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
            <span class="eyebrow">Model number</span>
            <Show
              when={edit.editing()}
              fallback={<div class="input input-bordered flex items-center text-sm font-data">{r().model_number}</div>}
            >
              <input class="input input-bordered w-full font-data" value={modelNumber()} onInput={(e) => setModelNumber(e.currentTarget.value)} />
            </Show>
          </div>
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Family</span>
            <Show
              when={edit.editing()}
              fallback={<div class="input input-bordered flex items-center text-sm">{r().family || "—"}</div>}
            >
              <input class="input input-bordered w-full" placeholder="TSW" value={family()} onInput={(e) => setFamily(e.currentTarget.value)} />
            </Show>
          </div>
          <div class="grid grid-cols-3 gap-3">
            <div class="flex flex-col gap-1.5">
              <span class="eyebrow">Released</span>
              <Show when={edit.editing()} fallback={<div class="input input-bordered flex items-center text-sm">{fmtDateOnly(r().released_at)}</div>}>
                <input type="date" class="input input-bordered w-full" value={releasedAt()} onInput={(e) => setReleasedAt(e.currentTarget.value)} />
              </Show>
            </div>
            <div class="flex flex-col gap-1.5">
              <span class="eyebrow">End of sale</span>
              <Show when={edit.editing()} fallback={<div class="input input-bordered flex items-center text-sm">{fmtDateOnly(r().eos_at)}</div>}>
                <input type="date" class="input input-bordered w-full" value={eosAt()} onInput={(e) => setEosAt(e.currentTarget.value)} />
              </Show>
            </div>
            <div class="flex flex-col gap-1.5">
              <span class="eyebrow">End of life</span>
              <Show when={edit.editing()} fallback={<div class="input input-bordered flex items-center text-sm">{fmtDateOnly(r().eol_at)}</div>}>
                <input type="date" class="input input-bordered w-full" value={eolAt()} onInput={(e) => setEolAt(e.currentTarget.value)} />
              </Show>
            </div>
          </div>
          <div class="grid grid-cols-2 gap-3">
            <ImageUploadField
              label="Front image"
              id={() => (edit.editing() ? frontImageId() : r().front_image_id)}
              editable={edit.editing}
              onUploaded={setFrontImageId}
            />
            <ImageUploadField
              label="Back image"
              id={() => (edit.editing() ? backImageId() : r().back_image_id)}
              editable={edit.editing}
              onUploaded={setBackImageId}
            />
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

// ImageUploadField shows a product photo (front or back), fetched by id and
// rendered as a data URL from a TRUSTED downloaded blob (never a user-typed
// URL), plus, when editable, a native file input that uploads the pick
// through the files API and reports the new file id back to the caller.
function ImageUploadField(p: {
  label: string;
  id: () => string | undefined;
  editable: () => boolean;
  onUploaded: (fileId: string) => void;
}): JSX.Element {
  const [dataUrl] = createResource(p.id, async (fid: string) => {
    const dl = await downloadFile(fid);
    return `data:${dl.content_type};base64,${dl.content}`;
  });
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  async function onPick(input: HTMLInputElement) {
    const f = input.files?.[0];
    input.value = "";
    if (!f) return;
    setErr(null);
    setBusy(true);
    try {
      const raw = await readAsDataUrl(f);
      const uploaded = await createFile({ name: f.name, contentType: f.type || "application/octet-stream", content: dataUrlToBase64(raw) });
      p.onUploaded(uploaded.id);
    } catch (e) {
      setErr(e instanceof Error ? e.message : describeError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div class="flex flex-col gap-1.5">
      <span class="eyebrow">{p.label}</span>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-xs"><span>{err()}</span></div>
      </Show>
      <Show
        when={dataUrl()}
        fallback={<div class="flex h-24 w-24 items-center justify-center rounded-box border border-dashed border-base-300 text-[11px] text-base-content/40">No image</div>}
      >
        {(url) => <img src={url()} alt={p.label} class="h-24 w-24 rounded-box border border-base-300 bg-base-100 object-contain" />}
      </Show>
      <Show when={p.editable()}>
        <input
          type="file"
          accept="image/*"
          class="file-input file-input-bordered file-input-sm w-full"
          aria-label={p.label}
          disabled={busy()}
          onChange={(e) => onPick(e.currentTarget)}
        />
      </Show>
    </div>
  );
}

// CreateModelForm: name the id (a kebab id, immutable after creation), pick the
// owning make (create-only), and set the display name and model number; family,
// lifecycle dates, and the front/back photos are optional.
export function CreateModelForm(p: { onCreated: (id: string) => void }): JSX.Element {
  const qc = useQueryClient();
  const makes = useQuery(() => ({ queryKey: COMPONENT_MAKES_KEY, queryFn: listMakes }));
  const [id, setId] = createSignal("");
  const [displayName, setDisplayName] = createSignal("");
  const [makeId, setMakeId] = createSignal("");
  const [modelNumber, setModelNumber] = createSignal("");
  const [family, setFamily] = createSignal("");
  const [releasedAt, setReleasedAt] = createSignal("");
  const [eosAt, setEosAt] = createSignal("");
  const [eolAt, setEolAt] = createSignal("");
  const [frontImageId, setFrontImageId] = createSignal<string | undefined>();
  const [backImageId, setBackImageId] = createSignal<string | undefined>();
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  async function submit(e: Event) {
    e.preventDefault();
    setBusy(true);
    setFormErr(null);
    try {
      const created = await createModel({
        id: id().trim(),
        display_name: displayName().trim(),
        make_id: makeId(),
        model_number: modelNumber().trim(),
        family: family().trim() || undefined,
        released_at: fromDateInput(releasedAt()),
        eos_at: fromDateInput(eosAt()),
        eol_at: fromDateInput(eolAt()),
        front_image_id: frontImageId(),
        back_image_id: backImageId(),
      });
      await qc.invalidateQueries({ queryKey: COMPONENT_MODELS_KEY });
      p.onCreated(created.id);
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
      <Field label="Id" hint="A kebab id, e.g. tsw-1070.">
        <input class="input input-bordered w-full font-data" value={id()} placeholder="tsw-1070" onInput={(e) => setId(e.currentTarget.value)} />
      </Field>
      <Field label="Display name">
        <input class="input input-bordered w-full" value={displayName()} placeholder="TSW-1070" onInput={(e) => setDisplayName(e.currentTarget.value)} />
      </Field>
      <Field label="Make" hint="The owning manufacturer; not editable after creation.">
        <select class="select select-bordered w-full" value={makeId()} onChange={(e) => setMakeId(e.currentTarget.value)}>
          <option value="" disabled>Choose a make…</option>
          <For each={makes.data ?? []}>{(m) => <option value={m.id}>{m.display_name}</option>}</For>
        </select>
      </Field>
      <Field label="Model number">
        <input class="input input-bordered w-full font-data" value={modelNumber()} placeholder="TSW-1070-B-S" onInput={(e) => setModelNumber(e.currentTarget.value)} />
      </Field>
      <Field label="Family" hint="Optional.">
        <input class="input input-bordered w-full" value={family()} placeholder="TSW" onInput={(e) => setFamily(e.currentTarget.value)} />
      </Field>
      <div class="grid grid-cols-3 gap-3">
        <Field label="Released" hint="Optional.">
          <input type="date" class="input input-bordered w-full" value={releasedAt()} onInput={(e) => setReleasedAt(e.currentTarget.value)} />
        </Field>
        <Field label="End of sale" hint="Optional.">
          <input type="date" class="input input-bordered w-full" value={eosAt()} onInput={(e) => setEosAt(e.currentTarget.value)} />
        </Field>
        <Field label="End of life" hint="Optional.">
          <input type="date" class="input input-bordered w-full" value={eolAt()} onInput={(e) => setEolAt(e.currentTarget.value)} />
        </Field>
      </div>
      <div class="grid grid-cols-2 gap-3">
        <ImageUploadField label="Front image" id={frontImageId} editable={() => true} onUploaded={setFrontImageId} />
        <ImageUploadField label="Back image" id={backImageId} editable={() => true} onUploaded={setBackImageId} />
      </div>
      <DrawerFooter>
        <Button type="submit" intent="action" icon={Plus} loading={busy()} disabled={busy() || !id().trim() || !displayName().trim() || !makeId() || !modelNumber().trim()}>Create model</Button>
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
