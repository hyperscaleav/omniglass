import { For, Show, createMemo } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { Fact } from "../components/DetailShell";
import { type BladeDef } from "../lib/blades";
import { type Role, ROLES_KEY, listRoles } from "../lib/principals";

// RoleDetail is a role's blade body: read-only this slice (custom-role editing is a
// later slice). It teaches the RBAC model against the real role by showing what the
// role inherits and its effective (flattened) permissions, the same PermGrid the
// catalog card used before Roles moved onto the shared list surface.

// groupPerms turns ["location:create,update", "system:read"] into a per-resource
// map for legible display, keeping the admin tier: `location:create,update` groups
// under location, `audit:read:admin` marks read as admin, and `>` (the superuser
// tail) surfaces as a single "everything" entry.
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

export function PermGrid(props: { perms: string[] }) {
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

export function RoleDetail(props: { id: string }) {
  const roles = useQuery(() => ({ queryKey: ROLES_KEY, queryFn: () => listRoles() }));
  const r = createMemo(() => (roles.data ?? []).find((x) => x.id === props.id) ?? null);
  return (
    <Show when={r()} fallback={<p class="py-8 text-center text-sm text-base-content/40">This role is no longer available.</p>}>
      {(role) => (
        <div class="flex flex-col gap-4">
          <div class="flex flex-wrap items-center gap-2">
            <span class="badge badge-ghost badge-sm font-data">{role().id}</span>
            <Show when={role().official}><span class="badge badge-soft badge-info badge-sm">official</span></Show>
          </div>
          <Show when={role().description}>
            <p class="text-sm text-base-content/70">{role().description}</p>
          </Show>
          <Show when={role().inherits.length}>
            <Fact label="Inherits" value={<span class="font-data text-sm text-base-content/70">{role().inherits.join(", ")}</span>} />
          </Show>
          <div>
            <div class="eyebrow mb-1.5">Effective permissions</div>
            <PermGrid perms={role().effective_permissions ?? role().permissions} />
          </div>
        </div>
      )}
    </Show>
  );
}

function RoleTitle(props: { id: string }) {
  const roles = useQuery(() => ({ queryKey: ROLES_KEY, queryFn: () => listRoles() }));
  const r = () => (roles.data ?? []).find((x) => x.id === props.id) as Role | undefined;
  return <>{r()?.display_name || r()?.id || "Role"}</>;
}

export const roleBlade: BladeDef = {
  Title: (p) => <RoleTitle id={p.id} />,
  Body: (p) => <RoleDetail id={p.id} />,
};
