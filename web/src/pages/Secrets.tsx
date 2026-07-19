import { For, Show, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import TreeSelect from "../components/TreeSelect";
import FieldRow from "../components/FieldRow";
import KVStacked from "../components/KVStacked";
import SecretFields from "../components/SecretFields";
import Button from "../components/Button";
import { DrawerFooter } from "../components/Drawer";
import { Plus } from "../components/icons";
import { type TreeNode } from "../lib/treeselect";
import {
  type Secret,
  type OwnerKind,
  SECRETS_KEY,
  SECRET_TYPES_KEY,
  listSecrets,
  listSecretTypes,
  createSecret,
  updateSecret,
  deleteSecret,
} from "../lib/secrets";
import { SYSTEMS_KEY, listSystems } from "../lib/systems";
import { LOCATIONS_KEY, listLocations } from "../lib/locations";
import { COMPONENTS_KEY, listComponents } from "../lib/components";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

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
  { key: "name", label: "Name", sortVal: (s) => s.name, cell: (s) => <span class="font-data font-semibold">{s.name}</span> },
  { key: "type", label: "Type", width: "170px", sortVal: (s) => s.secret_type, cell: (s) => <span class="badge badge-ghost badge-sm">{s.secret_type}</span> },
  { key: "owner", label: "Scope", width: "220px", sortVal: (s) => s.owner_kind, cell: (s) => <span class="text-base-content/70">{ownerLabel(s)}</span> },
  { key: "fields", label: "Fields", cell: (s) => fieldsPreview(s) },
];

export default function Secrets() {
  const me = useMe();
  const secrets = useQuery(() => ({ queryKey: SECRETS_KEY, queryFn: listSecrets }));

  const rows = createMemo(() => [...(secrets.data ?? [])].sort((a, b) => a.name.localeCompare(b.name)));

  return (
    <FlatList<Secret>
      config={{
        entity: { name: "secret", plural: "secrets" },
        rows,
        loading: () => secrets.isPending,
        error: () => secrets.error,
        filterKeys: [
          { key: "name", type: "string", hint: "substring", get: (s) => `${s.name} ${s.secret_type}`, values: () => [] },
          { key: "scope", type: "string", hint: "exact", get: (s) => s.owner_kind, values: (rs) => [...new Set(rs.map((r) => r.owner_kind))].sort() },
          { key: "type", type: "string", hint: "exact", get: (s) => s.secret_type, values: (rs) => [...new Set(rs.map((r) => r.secret_type))].sort() },
        ],
        filterPlaceholder: "filter secrets by name, type, scope…",
        columns,
        empty: "No secrets yet.",
        rowId: (s) => s.id,
        blades: { registry: { secret: secretBlade }, rootKind: "secret" },
        create: can(me.data, "secret", "create")
          ? { label: "New secret", can: () => can(me.data, "secret", "create"), body: (ctx) => <CreateSecretForm onCreated={ctx.close} /> }
          : undefined,
      }}
    />
  );
}

// secretBlade renders a secret on the shared blade stack (same chrome and footer
// action rail as the identity blades). The body is read-only in slice 1 (no field
// edit yet), so no pencil; the footer carries the one destructive action, Delete.
export const secretBlade: BladeDef = {
  Title: (p) => <SecretBladeTitle id={p.id} />,
  Body: (p) => <SecretBladeBody id={p.id} />,
};

function useSecretById(id: string): () => Secret | undefined {
  const secrets = useQuery(() => ({ queryKey: SECRETS_KEY, queryFn: listSecrets }));
  return () => (secrets.data ?? []).find((s) => s.id === id);
}

function SecretBladeTitle(p: { id: string }): JSX.Element {
  const secret = useSecretById(p.id);
  return <span class="font-data">{secret()?.name ?? "secret"}</span>;
}

function SecretBladeBody(p: { id: string }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const secret = useSecretById(p.id);
  const [err, setErr] = createSignal<string | null>(null);
  // The edit inputs, one per field. Non-secret fields seed with their current
  // value; secret fields start blank (masked), so a blank one is left unchanged.
  const [inputs, setInputs] = createSignal<Record<string, string>>({});

  createEffect(on(edit.editing, (editing) => {
    if (!editing) return;
    const seed: Record<string, string> = {};
    for (const f of secret()?.fields ?? []) seed[f.name] = f.secret ? "" : f.value;
    setInputs(seed);
    setErr(null);
  }));

  async function removeSecret() {
    const s = secret();
    if (!s) return;
    if (!confirm(`Delete secret "${s.name}" (${ownerLabel(s)})?`)) return;
    setErr(null);
    try {
      await deleteSecret(s.id);
      blades.close();
      await qc.invalidateQueries({ queryKey: SECRETS_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  async function save() {
    const s = secret();
    if (!s) return;
    // Send only the fields the operator filled; a blank secret field is left
    // unchanged (the server merges over the stored value).
    const fields: Record<string, string> = {};
    for (const [k, v] of Object.entries(inputs())) if (v !== "") fields[k] = v;
    setErr(null);
    try {
      await updateSecret(s.id, fields);
      await Promise.all([
        qc.invalidateQueries({ queryKey: SECRETS_KEY }),
        qc.invalidateQueries({ queryKey: ["effective-secrets"] }),
      ]);
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  // The footer action rail: Edit (pencil -> inline field edit -> Save/Cancel) for
  // secret:update, and Delete as the destructive action.
  edit.bind({
    editable: () => !!secret() && can(me.data, "secret", "update"),
    save,
    destructive: () => (secret() && can(me.data, "secret", "delete") ? { label: "Delete", tone: "danger", onClick: removeSecret } : undefined),
  });

  return (
    <Show when={secret()} fallback={<p class="text-sm text-base-content/50">Secret not found.</p>}>
      {(s) => (
        <div class="flex flex-col gap-4">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <KVStacked label="Type" value={<span class="badge badge-ghost badge-sm">{s().secret_type}</span>} />
            <KVStacked label="Scope" value={<span>{ownerLabel(s())}</span>} />
          </div>
          <div class="flex flex-col gap-1.5">
            <span class="eyebrow">Fields</span>
            <Show
              when={edit.editing()}
              fallback={<SecretFields secretId={s().id} fields={s().fields} canReveal={can(me.data, "secret", "reveal")} />}
            >
              <div class="flex flex-col gap-3">
                <For each={s().fields}>
                  {(f) => (
                    <FieldRow label={f.name} hint={f.secret ? "Leave blank to keep the current value." : undefined}>
                      <input
                        class="input input-bordered w-full font-data"
                        type={f.secret ? "password" : "text"}
                        placeholder={f.secret ? "••••••" : undefined}
                        value={inputs()[f.name] ?? ""}
                        onInput={(e) => setInputs({ ...inputs(), [f.name]: e.currentTarget.value })}
                      />
                    </FieldRow>
                  )}
                </For>
              </div>
            </Show>
          </div>
        </div>
      )}
    </Show>
  );
}

// CreateSecretForm: pick a type and a scope, then fill the type's operator
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
  // The owner picker is a tree: locations, systems, and components all nest by
  // parent_id, so the dropdown indents each candidate to its tier (the shared
  // TreeSelect, same as the location/parent pickers).
  const ownerTree = createMemo<TreeNode[]>(() => {
    switch (ownerKind()) {
      case "location": return (locations.data ?? []).map((l) => ({ id: l.id, value: l.name, label: l.display_name || l.name, parentId: l.parent_id }));
      case "system": return (systems.data ?? []).map((s) => ({ id: s.id, value: s.name, label: s.display_name || s.name, parentId: s.parent_id }));
      case "component": return (components.data ?? []).map((c) => ({ id: c.id, value: c.name, label: c.display_name || c.name, parentId: c.parent_id }));
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
    <form class="flex min-h-full flex-col gap-4" onSubmit={submit}>
      <Show when={formErr()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{formErr()}</span></div>
      </Show>
      <FieldRow label="Name" hint="The cascade key; unique per owner.">
        <input class="input input-bordered w-full font-data" value={name()} placeholder="poll-community" onInput={(e) => setName(e.currentTarget.value)} />
      </FieldRow>
      <FieldRow label="Type">
        <select class="select select-bordered w-full" value={typeId()} onChange={(e) => { setTypeId(e.currentTarget.value); setFields({}); }}>
          <option value="" disabled>Choose a type…</option>
          <For each={types.data}>{(t) => <option value={t.id}>{t.display_name}</option>}</For>
        </select>
      </FieldRow>
      <div class="grid grid-cols-2 gap-3">
        <FieldRow
          label="Scope"
          info="The estate scope this secret attaches to. It cascades down onto the components below it: global, or a location, system, or component."
          docHref="https://docs.omniglass.hyperscaleav.com/architecture/variables/"
        >
          <select class="select select-bordered w-full" value={ownerKind()} onChange={(e) => { setOwnerKind(e.currentTarget.value as OwnerKind); setOwner(""); }}>
            <For each={OWNER_KINDS}>{(k) => <option value={k}>{k.charAt(0).toUpperCase() + k.slice(1)}</option>}</For>
          </select>
        </FieldRow>
        <Show when={ownerKind() !== "global"}>
          <FieldRow label={ownerKind().charAt(0).toUpperCase() + ownerKind().slice(1)}>
            <TreeSelect items={ownerTree()} value={owner()} onChange={setOwner} rootLabel="Choose…" />
          </FieldRow>
        </Show>
      </div>
      <Show when={shape()}>
        <div class="flex flex-col gap-3 border-t border-base-300 pt-3">
          <span class="eyebrow">Fields</span>
          <For each={operatorFields()}>
            {(f) => (
              <FieldRow label={f.name} hint={f.secret ? "Encrypted at rest." : undefined}>
                <input
                  class="input input-bordered w-full font-data"
                  type={f.secret ? "password" : "text"}
                  value={fields()[f.name] ?? ""}
                  onInput={(e) => setFields({ ...fields(), [f.name]: e.currentTarget.value })}
                />
              </FieldRow>
            )}
          </For>
        </div>
      </Show>
      <DrawerFooter>
        <Button type="submit" intent="action" icon={Plus} disabled={busy() || !typeId() || !name().trim() || (ownerKind() !== "global" && !owner())}>Create secret</Button>
      </DrawerFooter>
    </form>
  );
}

