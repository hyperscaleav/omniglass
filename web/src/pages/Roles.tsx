import { Show, createMemo } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import { identityColumn } from "../components/IdentityCell";
import { type Role, ROLES_KEY, listRoles, roleFilterKeys, effectivePerms } from "../lib/principals";
import { identityRegistry } from "../lib/identityBlades";

// Roles: the catalog of roles on the shared FlatList surface (the same list shell
// and blade detail as Users and Groups), teaching the RBAC model against the real
// seeded roles. A row opens a read-only blade (the effective-permissions grid);
// custom-role editing is a later slice. Ordered by tier, least to most powerful,
// then anything custom.
const TIER = ["viewer", "operator", "deploy", "admin", "owner"];
const tierRank = (id: string) => {
  const i = TIER.indexOf(id);
  return i === -1 ? TIER.length : i;
};

const columns: FlatColumn<Role>[] = [
  identityColumn<Role>(),
  {
    key: "inherits", label: "Inherits", width: "200px", cell: (r) => (
      <Show when={r.inherits.length} fallback={<span class="text-base-content/30">—</span>}>
        <span class="font-data text-xs text-base-content/60">{r.inherits.join(", ")}</span>
      </Show>
    ),
  },
  {
    key: "perms", label: "Permissions", width: "130px", sortVal: (r) => effectivePerms(r).length,
    cell: (r) => <span class="tnum text-base-content/60">{effectivePerms(r).length}</span>,
  },
  // Origin rides its own column now that the name cell is the shared identity
  // treatment. It is the fact that separates a seeded role from one this fleet
  // wrote, which is what an operator is looking for once custom roles exist, so
  // it stays on the row rather than retiring with the old name cell.
  {
    key: "official", label: "Origin", width: "100px", sortVal: (r) => String(r.official),
    cell: (r) => <Show when={r.official}><span class="badge badge-soft badge-info badge-sm">official</span></Show>,
  },
];

export default function Roles() {
  const roles = useQuery(() => ({ queryKey: ROLES_KEY, queryFn: () => listRoles() }));
  const ordered = createMemo(() => [...(roles.data ?? [])].sort((a, b) => tierRank(a.name) - tierRank(b.name) || a.name.localeCompare(b.name)));

  return (
    <FlatList<Role>
      config={{
        entity: { name: "role", plural: "roles" },
        rows: ordered,
        loading: () => roles.isPending,
        error: () => roles.error,
        filterKeys: roleFilterKeys,
        filterPlaceholder: "filter roles by name or permission",
        columns,
        empty: "No roles yet.",
        rowId: (r) => r.id,
        blades: { registry: identityRegistry, rootKind: "role" },
      }}
    />
  );
}
