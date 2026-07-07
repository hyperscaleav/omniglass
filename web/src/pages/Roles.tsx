import { For, Show, createMemo } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import ListShell from "../components/ListShell";
import { type Role, ROLES_KEY, listRoles, roleFilterKeys } from "../lib/principals";

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
// map for legible display, keeping the admin tier: `location:create,update` groups
// under location, `audit:read:admin` marks read as admin, and `>` (the superuser
// tail) surfaces as a single "everything" entry. Tokens are `resource:action`
// or `resource:action:admin`; `*` is kept verbatim.
type PermAction = { action: string; admin: boolean };
function groupPerms(perms: string[]): { all: boolean; groups: { resource: string; actions: PermAction[] }[] } {
  let all = false;
  const by = new Map<string, Map<string, boolean>>();
  for (const p of perms) {
    if (p === ">") {
      all = true;
      continue;
    }
    const [resource, actionSeg, tier] = p.split(":");
    if (!resource || !actionSeg) continue;
    const admin = tier === "admin";
    const actions = by.get(resource) ?? new Map<string, boolean>();
    for (const a of actionSeg.split(",")) {
      const key = a.trim();
      actions.set(key, (actions.get(key) ?? false) || admin);
    }
    by.set(resource, actions);
  }
  const groups = [...by.entries()]
    .sort(([a], [b]) => (a === "*" ? -1 : b === "*" ? 1 : a.localeCompare(b)))
    .map(([resource, actions]) => ({ resource, actions: [...actions].map(([action, admin]) => ({ action, admin })) }));
  return { all, groups };
}

function PermGrid(props: { perms: string[] }) {
  const grouped = createMemo(() => groupPerms(props.perms));
  return (
    <div class="flex flex-wrap gap-1.5">
      <Show when={grouped().all}>
        <span class="inline-flex items-center gap-1 rounded-field border border-warning/40 bg-warning/10 py-[3px] pl-2 pr-2 font-data text-[11px] text-warning">
          <span class="font-bold">&gt;</span> everything
        </span>
      </Show>
      <For each={grouped().groups} fallback={<Show when={!grouped().all}><span class="text-xs text-base-content/40">No permissions.</span></Show>}>
        {(g) => (
          <span class="inline-flex items-center gap-1 rounded-field border border-base-300 bg-base-100 py-[3px] pl-2 pr-2 font-data text-[11px]">
            <span class="text-base-content/70">{g.resource}</span>
            <span class="text-base-content/30">:</span>
            <span class="inline-flex flex-wrap gap-x-1">
              <For each={g.actions}>
                {(a, i) => (
                  <span classList={{ "text-warning": a.action === "*", "text-base-content/90": a.action !== "*" }}>
                    {a.action}
                    <Show when={a.admin}><span class="ml-0.5 text-error/80" title="admin-sensitive">:admin</span></Show>
                    <Show when={i() < g.actions.length - 1}>,</Show>
                  </span>
                )}
              </For>
            </span>
          </span>
        )}
      </For>
    </div>
  );
}

function RoleCard(props: { role: Role }) {
  const r = props.role;
  return (
    <div class="rounded-box border border-base-300 bg-base-100 p-4">
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

  // No in-page H1 or subtitle: the top bar labels the page, matching the inventory
  // lists and Audit. The role cards teach the permission model themselves.
  return (
    <ListShell<Role>
      filterKeys={roleFilterKeys}
      rows={ordered()}
      placeholder="filter roles by name or permission"
      error={roles.error}
      errorLabel="Could not load roles"
    >
      {(filtered) => (
        <div class="grid gap-3 p-3">
          <For each={filtered()} fallback={<p class="text-sm text-base-content/40">No roles match.</p>}>
            {(r) => <RoleCard role={r} />}
          </For>
        </div>
      )}
    </ListShell>
  );
}
