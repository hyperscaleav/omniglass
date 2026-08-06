import { For, Show, type JSX } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import BladeTitle from "../components/BladeTitle";
import { identityColumn } from "../components/IdentityCell";
import KVStacked from "../components/KVStacked";
import { type SecretType, SECRET_TYPES_KEY, listSecretTypes } from "../lib/secret_types";
import { type BladeDef } from "../lib/blades";

// Secret types (Catalog > Secrets): the shapes a secret can take (snmp-community, basic-auth, and
// the rest), read-only by design. The registry is authoritative seed-owned
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

// Identity (the display name above the name, ADR-0062) and origin. No icon
// column: a secret type carries no glyph.
const columns: FlatColumn<SecretType>[] = [
  identityColumn<SecretType>(),
  { key: "official", label: "Origin", width: "100px", sortVal: (r) => String(r.official), cell: (r) => officialBadge(r.official) },
];

export default function SecretTypes() {
  const types = useQuery(() => ({ queryKey: SECRET_TYPES_KEY, queryFn: listSecretTypes }));

  // Sorted alphabetically by display name then name.
  const rows = () =>
    [...(types.data ?? [])].sort((a, b) => a.display_name.localeCompare(b.display_name) || a.name.localeCompare(b.name));

  return (
    <FlatList<SecretType>
      config={{
        entity: { name: "secret type", plural: "secret types" },
        rows,
        loading: () => types.isPending,
        error: () => types.error,
        filterKeys: [
          { key: "name", type: "string", hint: "substring", get: (r) => `${r.name} ${r.display_name}`, values: () => [] },
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

// secretTypeBlade renders one registry row read-only: the body never binds the
// edit slot, so no pencil and no destructive action render, whatever the caller
// holds. The fields list is the payload: what a secret of this type expects.
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
            <span class="text-[11px] text-base-content/40">Secret types are seeded with the release and read-only here; a release revision updates them.</span>
          </div>
        </div>
      )}
    </Show>
  );
}
