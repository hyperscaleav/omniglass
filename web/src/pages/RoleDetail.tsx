import { For, Show, createMemo, createSignal } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import KVStacked from "../components/KVStacked";
import { type BladeDef } from "../lib/blades";
import { type Role, ROLES_KEY, listRoles } from "../lib/principals";

// RoleDetail is a role's blade body: read-only this slice (custom-role editing is a
// later slice). It teaches the RBAC model against the real role by showing its NET
// permissions: what it holds alongside what it is missing, measured against the
// universe of every capability the API enforces (the same set published per-route
// in the OpenAPI spec as x-omniglass-permission).

type PermMode = "all" | "held" | "missing";

// isAdminTier flags a three-token admin-sensitive permission (e.g.
// audit:read:admin), so a held one can be tinted to signal its sensitivity.
function isAdminTier(perm: string): boolean {
  return perm.split(":").length >= 3;
}

// PermRow renders one permission, one line, no wrap: a status glyph (held vs not)
// and the full resource:action[:tier] string, lit when held and dimmed+struck when
// missing. Held admin-sensitive permissions carry a warning tint.
function PermRow(props: { perm: string; held: boolean }) {
  return (
    <div class="flex items-center gap-2 whitespace-nowrap font-data text-[11px] leading-5">
      <span class="w-3 shrink-0 text-center" classList={{ "text-success": props.held, "text-base-content/25": !props.held }}>
        {props.held ? "✓" : "·"}
      </span>
      <span
        classList={{
          "text-base-content/90": props.held && !isAdminTier(props.perm),
          "text-warning": props.held && isAdminTier(props.perm),
          "text-base-content/40 line-through": !props.held,
        }}
        title={isAdminTier(props.perm) ? "admin-sensitive" : undefined}
      >
        {props.perm}
      </span>
    </div>
  );
}

// PermGrid is the net permission view for a role: a flat, lexicographically sorted,
// one-per-line list of the permission universe, filtered to Held, Missing, or All.
// held is the server-resolved subset the role covers (wildcards, the :read floor,
// and the > tail already applied); missing is universe - held, computed here.
export function PermGrid(props: { universe: string[]; held: string[] }) {
  const [mode, setMode] = createSignal<PermMode>("held");
  const heldSet = createMemo(() => new Set(props.held));
  const universe = createMemo(() => [...props.universe].sort());
  const held = createMemo(() => universe().filter((p) => heldSet().has(p)));
  const missing = createMemo(() => universe().filter((p) => !heldSet().has(p)));
  const rows = createMemo(() => {
    const m = mode();
    if (m === "held") return held().map((perm) => ({ perm, held: true }));
    if (m === "missing") return missing().map((perm) => ({ perm, held: false }));
    return universe().map((perm) => ({ perm, held: heldSet().has(perm) }));
  });
  const emptyMsg = createMemo(() => {
    if (mode() === "missing") return "Holds every permission.";
    if (mode() === "held") return "Holds no permission.";
    return "No permissions.";
  });
  const tabs: { key: PermMode; label: string; count: () => number }[] = [
    { key: "all", label: "All", count: () => universe().length },
    { key: "held", label: "Held", count: () => held().length },
    { key: "missing", label: "Missing", count: () => missing().length },
  ];
  return (
    <div class="flex flex-col gap-2">
      <div role="tablist" class="tabs tabs-box tabs-xs w-fit">
        <For each={tabs}>
          {(t) => (
            <button
              role="tab"
              class="tab"
              classList={{ "tab-active": mode() === t.key }}
              onClick={() => setMode(t.key)}
            >
              {t.label}
              <span class="ml-1 tabular-nums text-base-content/50">{t.count()}</span>
            </button>
          )}
        </For>
      </div>
      <div class="flex max-h-80 flex-col gap-0.5 overflow-y-auto">
        <For each={rows()} fallback={<span class="text-xs text-base-content/40">{emptyMsg()}</span>}>
          {(row) => <PermRow perm={row.perm} held={row.held} />}
        </For>
      </div>
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
            <KVStacked label="Inherits" value={<span class="font-data text-sm text-base-content/70">{role().inherits.join(", ")}</span>} />
          </Show>
          <div>
            <div class="eyebrow mb-1.5">Permissions</div>
            <PermGrid universe={role().permission_universe ?? []} held={role().held ?? []} />
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
