import { entityLabel } from "../lib/entities";
import { For, Show, type JSX } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import BladeTitle from "../components/BladeTitle";
import { identityColumn } from "../components/IdentityCell";
import KVStacked from "../components/KVStacked";
import { type SecretType, SECRET_TYPES_KEY, listSecretTypes } from "../lib/secret_types";
import { type BladeDef, useBladeEdit } from "../lib/blades";
import { OFFICIAL_LOCK } from "../lib/catalog";

// Secret types (Catalog > Secrets): the shapes a secret can take (snmp-community, basic-auth, and
// the rest), read-only by design. The registry is authoritative release-owned
// reference data (the boot seed upserts it, so a release can correct it), so
// there is no create, no edit, and no delete here for anyone, owner included;
// the blade shows each type's declared fields. The whole page is gated by
// secret:read (a sensitive resource off the *:read viewer floor), the same
// permission that guards the secrets whose shape it teaches: a plain viewer
// does not see this page, and no longer loses the location registry over
// it (#598, which split the old joint Types page).

function officialBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

// Identity (the label above the name, ADR-0062) and origin. No icon
// column: a secret type carries no glyph.
const columns: FlatColumn<SecretType>[] = [
  identityColumn<SecretType>(),
  { key: "official", label: "Origin", width: "100px", sortVal: (r) => String(r.official), cell: (r) => officialBadge(r.official) },
];

export default function SecretTypes() {
  const types = useQuery(() => ({ queryKey: SECRET_TYPES_KEY, queryFn: listSecretTypes }));

  // Sorted alphabetically by label then name.
  const rows = () =>
    [...(types.data ?? [])].sort((a, b) => a.label.localeCompare(b.label) || a.name.localeCompare(b.name));

  return (
    <FlatList<SecretType>
      config={{
        entity: { name: "secret type", plural: "secret types" },
        rows,
        loading: () => types.isPending,
        error: () => types.error,
        filterKeys: [
          { key: "name", type: "string", hint: "substring", get: (r) => `${entityLabel(r)} ${r.name}`, values: () => [] },
          { key: "official", type: "string", hint: "exact", get: (r) => (r.official ? "official" : "custom"), values: () => ["official", "custom"] },
        ],
        filterPlaceholder: "filter secret types by name…",
        columns,
        empty: "No secret types.",
        rowId: (r) => r.name,
        blades: { registry: { secret_type: secretTypeBlade }, rootKind: "secret_type" },
        // No create: the registry is seeded-only, for every caller.
      }}
    />
  );
}

// secretTypeBlade renders one registry row read-only: the body binds only the
// edit slot's lock, so the footer's Edit / Delete pair renders greyed with the
// seeded-only reason on each tooltip, whatever the caller holds. The fields
// list is the payload: what a secret of this type expects.
export const secretTypeBlade: BladeDef = {
  Title: (p) => <SecretTypeBladeTitle id={p.id} />,
  Body: (p) => <SecretTypeBladeBody id={p.id} />,
};

function useSecretTypeRow(id: string): () => SecretType | undefined {
  const types = useQuery(() => ({ queryKey: SECRET_TYPES_KEY, queryFn: listSecretTypes }));
  return () => (types.data ?? []).find((r) => r.name === id);
}

function SecretTypeBladeTitle(p: { id: string }): JSX.Element {
  return <BladeTitle row={useSecretTypeRow(p.id)} fallback={p.id} />;
}

function SecretTypeBladeBody(p: { id: string }): JSX.Element {
  const row = useSecretTypeRow(p.id);

  // Always locked, for everyone: this registry has no write route at all, so
  // the reason is release ownership itself, not a permission. Same official
  // sentence as every other registry's official rows.
  useBladeEdit().bind({
    locked: () => (row() ? OFFICIAL_LOCK : null),
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Secret type not found.</p>}>
      {(r) => (
        <div class="flex flex-col gap-4">
          <div class="grid grid-cols-2 gap-3 text-sm">
            <KVStacked label="Origin" value={officialBadge(r().official)} />
            <KVStacked label="Id" value={<span class="font-data text-xs text-base-content/60">{r().id}</span>} />
          </div>
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
          </div>
        </div>
      )}
    </Show>
  );
}
