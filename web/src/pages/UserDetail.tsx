import { For, Show, createEffect, createMemo, createSignal, on } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import GrantBuilder from "../components/GrantBuilder";
import { Fact, RelatedList, DetailActions } from "../components/DetailShell";
import { useBlades, useBladeEdit } from "../lib/blades";
import type { BladeDef } from "../lib/blades";
import type { TreeNode } from "../lib/treeselect";
import type { ExistingGrant, GrantRef, ScopeOp } from "../lib/grantdraft";
import {
  type Principal, type ScopeKind, type UpdatePrincipal,
  PRINCIPALS_KEY, ROLES_KEY, listPrincipals, updatePrincipal, createGrant, revokeGrant, setPrincipalActive, listRoles,
  principalName, kindBadge, principalInitials,
} from "../lib/principals";
import { useMe, can } from "../lib/auth";
import { impersonate } from "../lib/impersonation";
import { describeError } from "../lib/format";
import { listLocations } from "../lib/locations";
import { listSystems } from "../lib/systems";
import { listComponents } from "../lib/components";

// UserDetail is a principal's blade body under the read -> Edit -> Save contract.
// Read mode shows the profile facts, the groups the user belongs to (a drill to the
// group when the Users page is the root), the grants (read-only chips), and the
// impersonate actions. The header pencil opens edit mode: the profile becomes
// inputs, the grant builder goes live, and the footer reveals Disable / Enable;
// Save commits the profile and grant changes as one session. A user is disabled,
// never deleted (the accounts-never-deleted invariant), so the destructive action
// is Disable.
export function UserDetail(props: { id: string }) {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const canUpdate = () => can(me.data, "principal", "update");
  const principals = useQuery(() => ({ queryKey: PRINCIPALS_KEY, queryFn: () => listPrincipals() }));
  const p = createMemo(() => principals.data?.find((x) => x.id === props.id) ?? null);
  const [actErr, setActErr] = createSignal<string | null>(null);

  const editing = () => edit.editing();
  const canDrillGroups = () => blades.stack()[0]?.kind === "user";

  const [username, setUsername] = createSignal("");
  const [displayName, setDisplayName] = createSignal("");
  const [email, setEmail] = createSignal("");
  let grantCommit: () => Promise<void> = async () => {};
  let grantCancel: () => void = () => {};

  // Seed the profile inputs from the principal when edit mode opens.
  createEffect(on(() => edit.editing(), (isEditing) => {
    if (isEditing) {
      const h = p()?.human;
      setUsername(h?.username ?? "");
      setDisplayName(h?.display_name ?? "");
      setEmail(h?.email ?? "");
    }
  }));

  edit.bind({
    editable: canUpdate,
    save: async () => {
      setActErr(null);
      try {
        const h = p()?.human;
        if (h) {
          const patch: UpdatePrincipal = {};
          if (username().trim() !== h.username) patch.username = username().trim();
          if (displayName().trim() !== (h.display_name ?? "")) patch.display_name = displayName().trim();
          if (email().trim() !== (h.email ?? "")) patch.email = email().trim();
          if (Object.keys(patch).length) await updatePrincipal(props.id, patch);
        }
        await grantCommit();
        await qc.invalidateQueries({ queryKey: PRINCIPALS_KEY });
      } catch (e) {
        setActErr(describeError(e));
        throw e; // keep edit mode open for a retry
      }
    },
    cancel: () => {
      setActErr(null);
      grantCancel();
    },
  });

  async function toggleActive(pr: Principal) {
    setActErr(null);
    try {
      await setPrincipalActive(pr.id, !pr.active);
      await qc.invalidateQueries({ queryKey: PRINCIPALS_KEY });
    } catch (e) {
      setActErr(describeError(e));
    }
  }

  async function doImpersonate(pr: Principal, mode: "view_as" | "act_as") {
    setActErr(null);
    const r = await impersonate(qc, pr.id, principalName(pr), mode);
    if (!r.ok) setActErr(r.message);
  }

  return (
    <Show when={p()} fallback={<p class="py-8 text-center text-sm text-base-content/40">This user is no longer available.</p>}>
      {(pr) => (
        <div class="flex flex-col gap-4">
          <div class="flex items-center gap-3">
            <div class="avatar avatar-placeholder">
              <div class="w-12 rounded-full bg-linear-to-br from-primary to-info text-primary-content">
                <span class="font-data text-sm font-bold uppercase">{principalInitials(pr())}</span>
              </div>
            </div>
            <span class="flex items-center gap-1.5">
              <span class={kindBadge(pr().kind)}>{pr().kind}</span>
              <Show when={!pr().active}><span class="badge badge-soft badge-warning badge-sm">inactive</span></Show>
            </span>
          </div>

          <Show when={actErr()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{actErr()}</span></div>
          </Show>

          {/* Profile: facts in read mode, inputs in edit mode. */}
          <Show
            when={editing() && pr().human}
            fallback={
              <div class="grid grid-cols-2 gap-3 text-sm">
                <Show when={pr().human}>
                  <Fact label="Username" value={<span class="font-data">{pr().human!.username}</span>} />
                  <Fact label="Email" value={pr().human!.email || <span class="text-base-content/40">not set</span>} />
                </Show>
                <Show when={pr().service}>
                  <Fact label="Label" value={<span class="font-data">{pr().service!.label}</span>} />
                </Show>
              </div>
            }
          >
            <div class="flex flex-col gap-3">
              <div>
                <label class="eyebrow mb-1.5 block" for="edit-username">Username</label>
                <input id="edit-username" autocomplete="off" class="input input-bordered w-full font-data" value={username()} onInput={(e) => setUsername(e.currentTarget.value)} />
              </div>
              <div>
                <label class="eyebrow mb-1.5 block" for="edit-display">Display name</label>
                <input id="edit-display" autocomplete="off" class="input input-bordered w-full" value={displayName()} onInput={(e) => setDisplayName(e.currentTarget.value)} />
              </div>
              <div>
                <label class="eyebrow mb-1.5 block" for="edit-email">Email</label>
                <input id="edit-email" type="email" autocomplete="off" class="input input-bordered w-full" value={email()} onInput={(e) => setEmail(e.currentTarget.value)} />
              </div>
            </div>
          </Show>

          {/* The groups the user belongs to: read-only, a drill to the group blade
              on the Users page. Membership is edited from the group, not here. */}
          <Show when={pr().groups?.length}>
            <RelatedList
              label="Groups"
              items={(pr().groups ?? []).map((g) => ({ id: g.id, kind: "group", name: g.name }))}
              empty="In no groups."
              onOpen={!editing() && canDrillGroups() ? (item) => blades.push({ kind: "group", id: item.id }) : undefined}
            />
          </Show>

          <GrantEditor
            principal={pr()}
            editing={editing()}
            canGrant={can(me.data, "principal_grant", "create")}
            canRevoke={can(me.data, "principal_grant", "delete")}
            onBind={(h) => { grantCommit = h.commit; grantCancel = h.cancel; }}
          />

          {/* Destructive action: Disable / Enable, in the edit footer. */}
          <Show when={editing() && canUpdate()}>
            <DetailActions
              destructive={
                <button class="btn btn-sm" classList={{ "btn-warn": pr().active, "btn-ok": !pr().active }} onClick={() => toggleActive(pr())}>
                  {pr().active ? "Disable" : "Enable"}
                </button>
              }
            />
          </Show>

          {/* Impersonate is a read-mode action (troubleshoot), not part of an edit session. */}
          <Show when={!editing() && can(me.data, "principal", "impersonate") && pr().id !== me.data?.principal?.id}>
            <div class="flex items-center gap-2 border-t border-base-300 pt-3">
              <span class="text-xs text-base-content/50">Impersonate to troubleshoot</span>
              <span class="flex-1" />
              <button class="btn btn-quiet btn-sm" onClick={() => doImpersonate(pr(), "view_as")}>View as</button>
              <button class="btn btn-warn btn-sm" onClick={() => doImpersonate(pr(), "act_as")}>Act as</button>
            </div>
          </Show>
        </div>
      )}
    </Show>
  );
}

// GrantEditor wires the shared GrantBuilder to a principal's direct grants. Read
// mode shows chips only (canGrant / canRevoke gated on `editing`); edit mode goes
// live and binds its commit up so the blade's Save applies the grant diff. Inherited
// grants (from a group) render read-only below in both modes and drill to the group.
function GrantEditor(props: { principal: Principal; editing: boolean; canGrant: boolean; canRevoke: boolean; onBind: (h: { commit: () => Promise<void>; cancel: () => void; dirty: () => boolean }) => void }) {
  const qc = useQueryClient();
  const blades = useBlades();
  const needTrees = () => props.canGrant || props.canRevoke;
  const roles = useQuery(() => ({ queryKey: ROLES_KEY, queryFn: listRoles, enabled: props.canGrant }));
  const locations = useQuery(() => ({ queryKey: ["locations"], queryFn: listLocations, enabled: needTrees() }));
  const systems = useQuery(() => ({ queryKey: ["systems"], queryFn: listSystems, enabled: needTrees() }));
  const components = useQuery(() => ({ queryKey: ["components"], queryFn: listComponents, enabled: needTrees() }));

  const nameOf = createMemo(() => {
    const m = new Map<string, string>();
    for (const e of locations.data ?? []) m.set(e.id, e.name);
    for (const e of systems.data ?? []) m.set(e.id, e.name);
    for (const e of components.data ?? []) m.set(e.id, e.name);
    return m;
  });

  const current = createMemo<ExistingGrant[]>(() =>
    props.principal.grants
      .filter((g) => g.id && !g.group_id)
      .map((g) => ({ id: g.id!, role: g.role, scope_kind: g.scope_kind as ScopeKind, scope_id: g.scope_id ?? undefined, scope_op: (g.scope_op as ScopeOp) || undefined })),
  );
  const inherited = createMemo(() => props.principal.grants.filter((g) => g.group_id));

  const entities = (kind: "location" | "system" | "component"): TreeNode[] => {
    const list = kind === "location" ? locations.data ?? [] : kind === "system" ? systems.data ?? [] : components.data ?? [];
    return list.map((e) => ({ id: e.id, value: e.id, label: e.name, parentId: e.parent_id, rank: 0 }));
  };

  async function onSave(diff: { adds: GrantRef[]; removes: ExistingGrant[] }) {
    for (const a of diff.adds) {
      await createGrant(props.principal.id, { role: a.role, scope_kind: a.scope_kind, scope_id: a.scope_kind === "all" ? undefined : a.scope_id, scope_op: a.scope_kind === "all" ? undefined : a.scope_op });
    }
    for (const r of diff.removes) {
      await revokeGrant(props.principal.id, r.id);
    }
    await qc.invalidateQueries({ queryKey: PRINCIPALS_KEY });
  }

  return (
    <div class="flex flex-col gap-2.5">
      <GrantBuilder
        principalId={props.principal.id}
        current={current()}
        roles={(roles.data ?? []).map((r) => ({ id: r.id, label: r.display_name || r.id, title: [r.description, `Grants: ${(r.effective_permissions ?? r.permissions).join(", ")}`].filter(Boolean).join("\n\n") }))}
        entities={entities}
        scopeName={(id) => nameOf().get(id)}
        canGrant={props.editing && props.canGrant}
        canRevoke={props.editing && props.canRevoke}
        onSave={onSave}
        bind={props.onBind}
      />
      <Show when={inherited().length}>
        <div class="flex flex-col gap-1">
          <div class="eyebrow">Inherited from groups</div>
          <div class="flex flex-wrap gap-1.5">
            <For each={inherited()}>
              {(g) => (
                <span
                  class="inline-flex items-center gap-1 rounded-field border border-dashed border-base-300 bg-base-100 py-[3px] pl-2.5 pr-2 text-xs"
                  title={`Inherited from the group "${g.group_name}". Edit the group to change it.`}
                >
                  <span class="font-data">{g.role} @ {g.scope_kind === "all" ? "all" : nameOf().get(g.scope_id ?? "") ?? g.scope_id}</span>
                  <span class="text-base-content/40">from</span>
                  <button class="text-primary hover:underline" title="Open this group" onClick={() => g.group_id && blades.push({ kind: "group", id: g.group_id })}>{g.group_name}</button>
                </span>
              )}
            </For>
          </div>
        </div>
      </Show>
    </div>
  );
}

function UserTitle(props: { id: string }) {
  const principals = useQuery(() => ({ queryKey: PRINCIPALS_KEY, queryFn: () => listPrincipals() }));
  const p = () => principals.data?.find((x) => x.id === props.id);
  return <>{p() ? principalName(p()!) : "User"}</>;
}

export const userBlade: BladeDef = {
  Title: (p) => <UserTitle id={p.id} />,
  Body: (p) => <UserDetail id={p.id} />,
};
