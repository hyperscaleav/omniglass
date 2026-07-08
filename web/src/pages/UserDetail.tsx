import { For, Show, createMemo, createSignal } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import GrantBuilder from "../components/GrantBuilder";
import { Fact, RelatedList, DetailActions } from "../components/DetailShell";
import { useBlades, type BladeDef } from "../lib/blades";
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

// UserDetail is a principal's blade body (rooted on the Users page, or a terminal
// blade opened from a group's member list). It re-derives the principal from the
// live query by id so a disable / enable or inline edit reflects immediately. It
// carries the profile facts, the groups the user belongs to (a drill to the group
// when the Users page is the root), the grant builder (direct grants editable,
// inherited read-only), disable / enable, edit, and impersonate.
export function UserDetail(props: { id: string }) {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const principals = useQuery(() => ({ queryKey: PRINCIPALS_KEY, queryFn: () => listPrincipals() }));
  const p = createMemo(() => principals.data?.find((x) => x.id === props.id) ?? null);
  const [editing, setEditing] = createSignal(false);
  const [actErr, setActErr] = createSignal<string | null>(null);

  // The user's groups drill to the group blade only when the Users page is the
  // stack root (user -> group). Opened as a terminal blade from a group's member
  // list (root = group), the groups are a read-only reference.
  const canDrillGroups = () => blades.stack()[0]?.kind === "user";

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

          <Show
            when={editing() && pr().human}
            fallback={
              <>
                <div class="grid grid-cols-2 gap-3 text-sm">
                  <Show when={pr().human}>
                    <Fact label="Username" value={<span class="font-data">{pr().human!.username}</span>} />
                    <Fact label="Email" value={pr().human!.email || <span class="text-base-content/40">not set</span>} />
                  </Show>
                  <Show when={pr().service}>
                    <Fact label="Label" value={<span class="font-data">{pr().service!.label}</span>} />
                  </Show>
                </div>

                <Show when={pr().groups?.length}>
                  <RelatedList
                    label="Groups"
                    items={(pr().groups ?? []).map((g) => ({ id: g.id, kind: "group", name: g.name }))}
                    empty="In no groups."
                    onOpen={canDrillGroups() ? (item) => blades.push({ kind: "group", id: item.id }) : undefined}
                  />
                </Show>

                <GrantEditor
                  principal={pr()}
                  canGrant={can(me.data, "principal_grant", "create")}
                  canRevoke={can(me.data, "principal_grant", "delete")}
                  onChange={() => qc.invalidateQueries({ queryKey: PRINCIPALS_KEY })}
                />

                <Show when={can(me.data, "principal", "update")}>
                  <DetailActions
                    destructive={
                      <button class="btn btn-sm" classList={{ "btn-warn": pr().active, "btn-ok": !pr().active }} onClick={() => toggleActive(pr())}>
                        {pr().active ? "Disable" : "Enable"}
                      </button>
                    }
                    primary={<Show when={pr().human}><button class="btn btn-action btn-sm" onClick={() => setEditing(true)}>Edit</button></Show>}
                  />
                </Show>
                <Show when={can(me.data, "principal", "impersonate") && pr().id !== me.data?.principal?.id}>
                  <div class="flex items-center gap-2 border-t border-base-300 pt-3">
                    <span class="text-xs text-base-content/50">Impersonate to troubleshoot</span>
                    <span class="flex-1" />
                    <button class="btn btn-quiet btn-sm" onClick={() => doImpersonate(pr(), "view_as")}>View as</button>
                    <button class="btn btn-warn btn-sm" onClick={() => doImpersonate(pr(), "act_as")}>Act as</button>
                  </div>
                </Show>
              </>
            }
          >
            <EditForm principal={pr()} onDone={() => setEditing(false)} />
          </Show>
        </div>
      )}
    </Show>
  );
}

// GrantEditor is the data edge for the grant builder: it fetches the role catalog
// and the scope-entity trees, renders the staged GrantBuilder over the principal's
// grants, and applies the saved diff (create the adds, revoke the removes). Adds
// run before removes so an owner swap never trips the last-owner guard mid-batch.
// The server enforces the owner invariant and answers 409.
function GrantEditor(props: { principal: Principal; canGrant: boolean; canRevoke: boolean; onChange: () => void | Promise<void> }) {
  const blades = useBlades();
  const needTrees = () => props.canGrant || props.canRevoke;
  const roles = useQuery(() => ({ queryKey: ROLES_KEY, queryFn: listRoles, enabled: props.canGrant }));
  const locations = useQuery(() => ({ queryKey: ["locations"], queryFn: listLocations, enabled: needTrees() }));
  const systems = useQuery(() => ({ queryKey: ["systems"], queryFn: listSystems, enabled: needTrees() }));
  const components = useQuery(() => ({ queryKey: ["components"], queryFn: listComponents, enabled: needTrees() }));

  // id -> name across the tree tiers, for turning a stored scope id back into a
  // readable grant chip.
  const nameOf = createMemo(() => {
    const m = new Map<string, string>();
    for (const e of locations.data ?? []) m.set(e.id, e.name);
    for (const e of systems.data ?? []) m.set(e.id, e.name);
    for (const e of components.data ?? []) m.set(e.id, e.name);
    return m;
  });

  // Only DIRECT grants are editable here: an inherited grant (from a group) is
  // revoked by editing the group, not the member, so it never enters the builder's
  // draft set. Inherited grants render read-only below.
  const current = createMemo<ExistingGrant[]>(() =>
    props.principal.grants
      .filter((g) => g.id && !g.group_id)
      .map((g) => ({ id: g.id!, role: g.role, scope_kind: g.scope_kind as ScopeKind, scope_id: g.scope_id ?? undefined, scope_op: (g.scope_op as ScopeOp) || undefined })),
  );
  // Grants inherited from a group: read-only, tagged with their source group.
  const inherited = createMemo(() => props.principal.grants.filter((g) => g.group_id));

  // The scope entities of a kind as TreeNodes, so the entity stage reads as an
  // indented tree (value = id, ordered by parent_id) not a flat list.
  const entities = (kind: "location" | "system" | "component"): TreeNode[] => {
    const list = kind === "location" ? locations.data ?? [] : kind === "system" ? systems.data ?? [] : components.data ?? [];
    return list.map((e) => ({ id: e.id, value: e.id, label: e.name, parentId: e.parent_id, rank: 0 }));
  };

  async function onSave(diff: { adds: GrantRef[]; removes: ExistingGrant[] }) {
    try {
      for (const a of diff.adds) {
        await createGrant(props.principal.id, {
          role: a.role,
          scope_kind: a.scope_kind,
          scope_id: a.scope_kind === "all" ? undefined : a.scope_id,
          scope_op: a.scope_kind === "all" ? undefined : a.scope_op,
        });
      }
      for (const r of diff.removes) {
        await revokeGrant(props.principal.id, r.id);
      }
    } finally {
      await props.onChange();
    }
  }

  return (
    <div class="flex flex-col gap-2.5">
      <GrantBuilder
        principalId={props.principal.id}
        current={current()}
        roles={(roles.data ?? []).map((r) => ({
          id: r.id,
          label: r.display_name || r.id,
          title: [r.description, `Grants: ${(r.effective_permissions ?? r.permissions).join(", ")}`].filter(Boolean).join("\n\n"),
        }))}
        entities={entities}
        scopeName={(id) => nameOf().get(id)}
        canGrant={props.canGrant}
        canRevoke={props.canRevoke}
        onSave={onSave}
      />
      {/* Inherited grants are read-only here: they come from a group, so they are
          changed by editing the group, not the member. The source group drills to
          its blade (a group blade over this user), the same as the Groups list. */}
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

// EditForm edits a human principal's admin-owned fields inline in the blade (no
// nested dialog): display name, email, and username. Username is renamable on this
// admin page (it is not a key, and the whole edit is gated by principal:update).
// Only the changed fields are sent; on save it invalidates the directory and
// returns to the read view.
function EditForm(props: { principal: Principal; onDone: () => void }) {
  const qc = useQueryClient();
  const h = props.principal.human!;
  const [username, setUsername] = createSignal(h.username);
  const [displayName, setDisplayName] = createSignal(h.display_name ?? "");
  const [email, setEmail] = createSignal(h.email ?? "");
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      const patch: UpdatePrincipal = {};
      if (username().trim() !== h.username) patch.username = username().trim();
      if (displayName().trim() !== (h.display_name ?? "")) patch.display_name = displayName().trim();
      if (email().trim() !== (h.email ?? "")) patch.email = email().trim();
      await updatePrincipal(props.principal.id, patch);
      await qc.invalidateQueries({ queryKey: PRINCIPALS_KEY });
      props.onDone();
    } catch (er) {
      setErr(describeError(er));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form class="flex flex-col gap-3" onSubmit={submit}>
      <p class="text-xs text-base-content/50">Change this user's display name, email, or username. Renaming is safe: their credentials and grants follow the account.</p>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <div>
        <label class="eyebrow mb-1.5 block" for="edit-username">Username</label>
        <input id="edit-username" autocomplete="off" class="input input-bordered w-full font-data" value={username()} onInput={(e) => setUsername(e.currentTarget.value)} disabled={busy()} required />
      </div>
      <div>
        <label class="eyebrow mb-1.5 block" for="edit-display">Display name</label>
        <input id="edit-display" autocomplete="off" class="input input-bordered w-full" value={displayName()} onInput={(e) => setDisplayName(e.currentTarget.value)} disabled={busy()} />
      </div>
      <div>
        <label class="eyebrow mb-1.5 block" for="edit-email">Email</label>
        <input id="edit-email" type="email" autocomplete="off" class="input input-bordered w-full" value={email()} onInput={(e) => setEmail(e.currentTarget.value)} disabled={busy()} />
      </div>
      <div class="mt-1 flex justify-end gap-2">
        <button type="button" class="btn btn-quiet btn-sm" onClick={props.onDone} disabled={busy()}>Cancel</button>
        <button type="submit" class="btn btn-action btn-sm" disabled={busy() || !username().trim()}>
          <Show when={busy()}><span class="loading loading-spinner loading-xs" /></Show>
          Save changes
        </button>
      </div>
    </form>
  );
}

// The blade title reads the live principal name by id.
function UserTitle(props: { id: string }) {
  const principals = useQuery(() => ({ queryKey: PRINCIPALS_KEY, queryFn: () => listPrincipals() }));
  const p = () => principals.data?.find((x) => x.id === props.id);
  return <>{p() ? principalName(p()!) : "User"}</>;
}

export const userBlade: BladeDef = {
  Title: (p) => <UserTitle id={p.id} />,
  Body: (p) => <UserDetail id={p.id} />,
};
