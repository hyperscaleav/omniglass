import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import { identityColumn } from "../components/IdentityCell";
import BladeTitle from "../components/BladeTitle";
import FieldRow from "../components/FieldRow";
import BladeField from "../components/BladeField";
import KVStacked from "../components/KVStacked";
import SecretFields from "../components/SecretFields";
import CreateSecretForm from "../components/CreateSecretForm";
import {
  type Secret,
  SECRETS_KEY,
  listSecrets,
  updateSecret,
  deleteSecret,
} from "../lib/secrets";
import { useMe, can, canAtPlatform, platformTierGap, platformAuthorityHint } from "../lib/auth";
import { describeError } from "../lib/format";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Secrets: the shared-credential directory on the FlatList surface. A secret is a
// typed, encrypted-at-rest value owned at one scope (platform, or a location /
// component; the system band is retired, ADR-0052) and resolved down the cascade; this page is the admin
// directory (create, inspect masked, delete). A secret reaches a component by
// being sourced into a field; this directory manages the cells themselves.

function ownerLabel(s: Secret): string {
  if (s.owner_kind === "platform") return "Platform";
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
  identityColumn<Secret>(),
  { key: "type", label: "Type", width: "170px", sortVal: (s) => s.secret_type, cell: (s) => <span class="badge badge-ghost badge-sm">{s.secret_type}</span> },
  { key: "owner", label: "Scope", width: "220px", sortVal: (s) => s.owner_kind, cell: (s) => <span class="text-base-content/70">{ownerLabel(s)}</span> },
  { key: "fields", label: "Fields", cell: (s) => fieldsPreview(s) },
];

export default function Secrets() {
  const me = useMe();
  const secrets = useQuery(() => ({ queryKey: SECRETS_KEY, queryFn: listSecrets }));

  // Rendered in the order ListSecrets serves, on both arms of its query (#613, #757).
  const rows = createMemo(() => secrets.data ?? []);

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
          ? { label: "New secret", can: () => can(me.data, "secret", "create"), body: (ctx) => <CreateSecretForm onCreated={ctx.select} /> }
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

// The heading is the label, falling back to the name in the data face, through
// the one blade-heading primitive. It was a bare span until the entity gained a
// label (#613), which is the moment #581 said to switch.
function SecretBladeTitle(p: { id: string }): JSX.Element {
  const row = useSecretById(p.id);
  return <BladeTitle row={row} fallback="secret" />;
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
  const [label, setLabel] = createSignal("");

  // Fill the drafts from the row as it stands; bound below, the slot runs this
  // on entering edit and again on reseed (#748).
  const seedDrafts = () => {
    const seed: Record<string, string> = {};
    for (const f of secret()?.fields ?? []) seed[f.name] = f.secret ? "" : f.value;
    setInputs(seed);
    // The RAW column, never entityLabel: seeding the editor with the fallback
    // would turn "no label" into a label the operator never typed.
    setLabel(secret()?.label ?? "");
    setErr(null);
  };

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
      await updateSecret(s.id, { fields, label: label() });
      await qc.invalidateQueries({ queryKey: SECRETS_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  // A secret at the platform tier is install-wide, so the server gates its write on
  // platform:<action> on top of secret:<action>. The footer gates on the same pair,
  // so an operator never earns a 403 from a control the blade offered; below the
  // tier the resource permission alone still decides.
  const mayWrite = (action: string) => {
    const s = secret();
    if (!s) return false;
    return s.owner_kind === "platform" ? canAtPlatform(me.data, "secret", action) : can(me.data, "secret", action);
  };
  // Holding the resource half without the tier half is the one state worth
  // explaining: the actions are gone and nothing else on the blade says why.
  const tierGap = () => (secret()?.owner_kind === "platform" ? platformTierGap(me.data, "secret", ["update", "delete"]) : []);

  // The footer action rail: Edit (pencil -> inline field edit -> Save/Cancel) for
  // secret:update, and Delete as the destructive action.
  edit.bind({
    editable: () => mayWrite("update"),
    seed: seedDrafts,
    save,
    destructive: () => (mayWrite("delete") ? { label: "Delete", tone: "danger", onClick: removeSecret } : undefined),
  });

  return (
    <Show when={secret()} fallback={<p class="text-sm text-base-content/50">Secret not found.</p>}>
      {(s) => (
        <div class="flex flex-col gap-4">
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <Show when={tierGap().length > 0}>
            <div role="status" class="alert alert-info alert-soft text-sm">
              <span>{platformAuthorityHint("A secret", tierGap())}</span>
            </div>
          </Show>
          <BladeField bind="label" value={() => s().label ?? ""} draft={label} onInput={setLabel} />
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

