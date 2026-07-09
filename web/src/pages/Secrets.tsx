import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import {
  type Secret,
  type OwnerKind,
  SECRETS_KEY,
  SECRET_TYPES_KEY,
  listSecrets,
  listSecretTypes,
  createSecret,
  deleteSecret,
} from "../lib/secrets";
import { SYSTEMS_KEY, listSystems } from "../lib/systems";
import { LOCATIONS_KEY, listLocations } from "../lib/locations";
import { COMPONENTS_KEY, listComponents } from "../lib/components";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";

// Secrets: the shared-credential directory on the FlatList surface. A secret is a
// typed, encrypted-at-rest value owned at one scope (global, or a location /
// system / component) and resolved down the cascade; this page is the admin
// directory (create, inspect masked, delete). The per-component effective view
// (which value actually wins where) is the cascade panel on a component's detail.

const OWNER_KINDS: OwnerKind[] = ["global", "location", "system", "component"];

function ownerLabel(s: Secret): string {
  if (s.owner_kind === "global") return "Global";
  const tier = s.owner_kind.charAt(0).toUpperCase() + s.owner_kind.slice(1);
  return s.owner_name ? `${tier}: ${s.owner_name}` : tier;
}

function fieldsPreview(s: Secret): JSX.Element {
  return (
    <span class="font-data text-xs text-base-content/60">
      <For each={s.fields}>
        {(f, i) => (
          <>
            <Show when={i()}><span class="text-base-content/30">, </span></Show>
            <span class="text-base-content/45">{f.name}</span>=<span classList={{ "text-base-content/40": f.secret }}>{f.value}</span>
          </>
        )}
      </For>
    </span>
  );
}

const columns: FlatColumn<Secret>[] = [
  {
    key: "name", label: "Name", sortVal: (s) => s.name, cell: (s) => (
      <span class="inline-flex items-center gap-2">
        <span class="font-data font-semibold">{s.name}</span>
        <span class="badge badge-ghost badge-sm">{s.secret_type}</span>
      </span>
    ),
  },
  { key: "owner", label: "Owner", width: "220px", sortVal: (s) => s.owner_kind, cell: (s) => <span class="text-base-content/70">{ownerLabel(s)}</span> },
  { key: "fields", label: "Fields", cell: (s) => fieldsPreview(s) },
];

export default function Secrets() {
  const qc = useQueryClient();
  const me = useMe();
  const secrets = useQuery(() => ({ queryKey: SECRETS_KEY, queryFn: listSecrets }));

  const rows = createMemo(() => [...(secrets.data ?? [])].sort((a, b) => a.name.localeCompare(b.name)));

  const [err, setErr] = createSignal<string | null>(null);
  async function del(s: Secret) {
    if (!confirm(`Delete secret "${s.name}" (${ownerLabel(s)})?`)) return;
    setErr(null);
    try {
      await deleteSecret(s.id);
      await qc.invalidateQueries({ queryKey: SECRETS_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  return (
    <>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <FlatList<Secret>
        config={{
          entity: { name: "secret", plural: "secrets" },
          rows,
          loading: () => secrets.isPending,
          error: () => secrets.error,
          filterKeys: [
            { key: "name", type: "string", hint: "substring", get: (s) => `${s.name} ${s.secret_type}`, values: () => [] },
            { key: "owner", type: "string", hint: "exact", get: (s) => s.owner_kind, values: (rs) => [...new Set(rs.map((r) => r.owner_kind))].sort() },
            { key: "type", type: "string", hint: "exact", get: (s) => s.secret_type, values: (rs) => [...new Set(rs.map((r) => r.secret_type))].sort() },
          ],
          filterPlaceholder: "filter secrets by name, type, owner…",
          columns,
          empty: "No secrets yet.",
          rowId: (s) => s.id,
          detail: (s) => ({
            title: <span class="font-data">{s.name}</span>,
            body: <SecretDetail secret={s} onDelete={() => del(s)} canDelete={can(me.data, "secret", "delete")} />,
          }),
          create: can(me.data, "secret", "create")
            ? { label: "New secret", can: () => can(me.data, "secret", "create"), body: (ctx) => <CreateSecretForm onCreated={ctx.close} /> }
            : undefined,
        }}
      />
    </>
  );
}

function SecretDetail(p: { secret: Secret; onDelete: () => void; canDelete: boolean }): JSX.Element {
  return (
    <div class="flex flex-col gap-4">
      <div class="grid grid-cols-2 gap-3 text-sm">
        <Fact label="Type"><span class="badge badge-soft badge-neutral badge-sm">{p.secret.secret_type}</span></Fact>
        <Fact label="Owner"><span>{ownerLabel(p.secret)}</span></Fact>
      </div>
      <div class="flex flex-col gap-1.5">
        <span class="eyebrow">Fields</span>
        <div class="overflow-hidden rounded-box border border-base-300">
          <For each={p.secret.fields}>
            {(f, i) => (
              <div class="flex items-center gap-2 px-3 py-2 text-sm" classList={{ "border-t border-base-300": i() > 0 }}>
                <span class="font-data text-base-content/60">{f.name}</span>
                <span class="flex-1" />
                <span class="font-data" classList={{ "text-base-content/40": f.secret }}>{f.value}</span>
                <Show when={f.secret}><span class="badge badge-ghost badge-sm text-[10px]">secret</span></Show>
              </div>
            )}
          </For>
        </div>
        <span class="text-[11px] text-base-content/40">Secret fields are encrypted at rest and shown masked.</span>
      </div>
      <Show when={p.canDelete}>
        <div class="border-t border-base-300 pt-3">
          <button class="btn btn-danger btn-sm" onClick={() => p.onDelete()}>Delete secret</button>
        </div>
      </Show>
    </div>
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

// CreateSecretForm: pick a type and an owner scope, then fill the type's operator
// fields. Secret fields use a password input; the values are sealed server-side.
function CreateSecretForm(p: { onCreated: () => void }): JSX.Element {
  const qc = useQueryClient();
  const types = useQuery(() => ({ queryKey: SECRET_TYPES_KEY, queryFn: listSecretTypes }));
  const systems = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));

  const [name, setName] = createSignal("");
  const [typeId, setTypeId] = createSignal("");
  const [ownerKind, setOwnerKind] = createSignal<OwnerKind>("global");
  const [owner, setOwner] = createSignal("");
  const [fields, setFields] = createSignal<Record<string, string>>({});
  const [busy, setBusy] = createSignal(false);
  const [formErr, setFormErr] = createSignal<string | null>(null);

  const shape = createMemo(() => (types.data ?? []).find((t) => t.id === typeId()));
  // The fields the operator fills (lifecycle-origin fields are set by the secret's
  // own machinery, never at creation).
  const operatorFields = createMemo(() => (shape()?.fields ?? []).filter((f) => f.origin !== "lifecycle"));
  const ownerOptions = createMemo(() => {
    switch (ownerKind()) {
      case "location": return (locations.data ?? []).map((l) => ({ value: l.name, label: l.display_name || l.name }));
      case "system": return (systems.data ?? []).map((s) => ({ value: s.name, label: s.display_name || s.name }));
      case "component": return (components.data ?? []).map((c) => ({ value: c.name, label: c.display_name || c.name }));
      default: return [];
    }
  });

  async function submit(e: Event) {
    e.preventDefault();
    setBusy(true);
    setFormErr(null);
    try {
      await createSecret({
        name: name().trim(),
        secret_type: typeId(),
        owner_kind: ownerKind(),
        owner: ownerKind() === "global" ? undefined : owner() || undefined,
        fields: fields(),
      });
      await qc.invalidateQueries({ queryKey: SECRETS_KEY });
      p.onCreated();
    } catch (er) {
      setFormErr(describeError(er));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form class="flex flex-col gap-4" onSubmit={submit}>
      <Show when={formErr()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{formErr()}</span></div>
      </Show>
      <Field label="Name" hint="The cascade key; unique per owner.">
        <input class="input input-bordered w-full font-data" value={name()} placeholder="poll-community" onInput={(e) => setName(e.currentTarget.value)} />
      </Field>
      <Field label="Type">
        <select class="select select-bordered w-full" value={typeId()} onChange={(e) => { setTypeId(e.currentTarget.value); setFields({}); }}>
          <option value="" disabled>Choose a type…</option>
          <For each={types.data}>{(t) => <option value={t.id}>{t.display_name}</option>}</For>
        </select>
      </Field>
      <div class="grid grid-cols-2 gap-3">
        <Field label="Owner scope">
          <select class="select select-bordered w-full" value={ownerKind()} onChange={(e) => { setOwnerKind(e.currentTarget.value as OwnerKind); setOwner(""); }}>
            <For each={OWNER_KINDS}>{(k) => <option value={k}>{k.charAt(0).toUpperCase() + k.slice(1)}</option>}</For>
          </select>
        </Field>
        <Show when={ownerKind() !== "global"}>
          <Field label="Owner">
            <select class="select select-bordered w-full" value={owner()} onChange={(e) => setOwner(e.currentTarget.value)}>
              <option value="" disabled>Choose…</option>
              <For each={ownerOptions()}>{(o) => <option value={o.value}>{o.label}</option>}</For>
            </select>
          </Field>
        </Show>
      </div>
      <Show when={shape()}>
        <div class="flex flex-col gap-3 border-t border-base-300 pt-3">
          <span class="eyebrow">Fields</span>
          <For each={operatorFields()}>
            {(f) => (
              <Field label={f.name} hint={f.secret ? "Encrypted at rest." : undefined}>
                <input
                  class="input input-bordered w-full font-data"
                  type={f.secret ? "password" : "text"}
                  value={fields()[f.name] ?? ""}
                  onInput={(e) => setFields({ ...fields(), [f.name]: e.currentTarget.value })}
                />
              </Field>
            )}
          </For>
        </div>
      </Show>
      <div class="mt-1 flex justify-end gap-2">
        <button type="submit" class="btn btn-action btn-sm" disabled={busy() || !typeId() || !name().trim()}>Create secret</button>
      </div>
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
