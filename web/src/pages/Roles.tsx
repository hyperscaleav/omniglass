import { For, Show, createMemo } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import { type Role, ROLES_KEY, listRoles } from "../lib/principals";
import { describeError } from "../lib/format";

// Roles: the read-only catalog of roles, teaching the RBAC model against the real
// seeded roles. Each card shows a role's display name, description, what it
// inherits, and its effective (flattened) permissions, so an operator can see
// exactly what a role grants before assigning it. Custom-role editing is a later
// slice; this is read + metadata only.

// Order roles by tier for reading, least to most powerful, then anything custom.
const TIER = ["viewer", "operator", "deploy", "admin", "owner"];
const tierRank = (id: string) => {
  const i = TIER.indexOf(id);
  return i === -1 ? TIER.length : i;
};

// groupPerms turns ["location:create,update", "system:read"] into a per-resource
// map for legible display: { location: ["create","update"], system: ["read"] }.
// A "*" resource or action is kept verbatim.
function groupPerms(perms: string[]): { resource: string; actions: string[] }[] {
  const by = new Map<string, Set<string>>();
  for (const p of perms) {
    const [resource, actions] = p.split(":");
    if (!resource || !actions) continue;
    const set = by.get(resource) ?? new Set<string>();
    for (const a of actions.split(",")) set.add(a.trim());
    by.set(resource, set);
  }
  return [...by.entries()]
    .sort(([a], [b]) => (a === "*" ? -1 : b === "*" ? 1 : a.localeCompare(b)))
    .map(([resource, actions]) => ({ resource, actions: [...actions] }));
}

function PermGrid(props: { perms: string[] }) {
  const groups = createMemo(() => groupPerms(props.perms));
  return (
    <div class="flex flex-wrap gap-1.5">
      <For each={groups()} fallback={<span class="text-xs text-base-content/40">No permissions.</span>}>
        {(g) => (
          <span class="inline-flex items-center gap-1 rounded-field border border-base-300 bg-base-100 py-[3px] pl-2 pr-2 font-data text-[11px]">
            <span class="text-base-content/70">{g.resource}</span>
            <span class="text-base-content/30">:</span>
            <span classList={{ "text-warning": g.actions.includes("*"), "text-base-content/90": !g.actions.includes("*") }}>{g.actions.join(", ")}</span>
          </span>
        )}
      </For>
    </div>
  );
}

function RoleCard(props: { role: Role }) {
  const r = props.role;
  return (
    <div class="rounded-box border border-base-300 bg-base-200/40 p-4">
      <div class="flex flex-wrap items-center gap-2">
        <h2 class="text-base font-semibold">{r.display_name || r.id}</h2>
        <span class="badge badge-ghost badge-sm font-data">{r.id}</span>
        <Show when={r.official}>
          <span class="badge badge-soft badge-info badge-sm">official</span>
        </Show>
        <Show when={r.inherits.length}>
          <span class="ml-auto text-xs text-base-content/50">
            inherits <span class="font-data text-base-content/70">{r.inherits.join(", ")}</span>
          </span>
        </Show>
      </div>
      <Show when={r.description}>
        <p class="mt-1.5 text-sm text-base-content/70">{r.description}</p>
      </Show>
      <div class="mt-3">
        <div class="eyebrow mb-1.5">Effective permissions</div>
        <PermGrid perms={r.effective_permissions ?? r.permissions} />
      </div>
    </div>
  );
}

export default function Roles() {
  const roles = useQuery(() => ({ queryKey: ROLES_KEY, queryFn: () => listRoles() }));
  const ordered = createMemo(() => [...(roles.data ?? [])].sort((a, b) => tierRank(a.id) - tierRank(b.id) || a.id.localeCompare(b.id)));

  return (
    <Page title="Roles">
      <p class="mb-4 text-sm text-base-content/60">
        The roles a grant can assign. A role is a bundle of <span class="font-data">resource:action</span> permissions;
        permissions are additive and inherit, and every role reads what it can act on (the read floor). These are
        the built-in roles; custom roles are coming.
      </p>

      <Show when={roles.error}>
        <div role="alert" class="alert alert-error alert-soft mb-4 text-sm"><span>{describeError(roles.error)}</span></div>
      </Show>

      <div class="grid gap-3">
        <For each={ordered()} fallback={<p class="text-sm text-base-content/40">{roles.isLoading ? "Loading…" : "No roles."}</p>}>
          {(r) => <RoleCard role={r} />}
        </For>
      </div>
    </Page>
  );
}
