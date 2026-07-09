import { type JSX, For, Show, createEffect, createMemo, createSignal, on } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import GrantBuilder from "../components/GrantBuilder";
import { Fact, RelatedList } from "../components/DetailShell";
import { Ban, Eye, Key, Mask, Trash, X } from "../components/icons";
import PasswordField from "../components/PasswordField";
import { useBlades, useBladeEdit } from "../lib/blades";
import type { BladeDef } from "../lib/blades";
import type { TreeNode } from "../lib/treeselect";
import type { ExistingGrant, GrantRef, ScopeOp } from "../lib/grantdraft";
import {
  type Principal, type ScopeKind, type UpdatePrincipal,
  PRINCIPALS_KEY, ROLES_KEY, getPrincipal, updatePrincipal, createGrant, revokeGrant, setPrincipalActive, listRoles,
  archivePrincipal, restorePrincipal, purgePrincipal, resetPrincipalPassword, consumePendingPrincipalEdit,
  principalName, kindBadge, principalInitials,
} from "../lib/principals";
import { useMe, can } from "../lib/auth";
import { impersonate } from "../lib/impersonation";
import { describeError } from "../lib/format";
import { handleError, emailError, passwordError, isPasswordPolicyMessage } from "../lib/validate";
import { listLocations } from "../lib/locations";
import { listSystems } from "../lib/systems";
import { listComponents } from "../lib/components";

// UserDetail is a principal's blade body under the read -> Edit -> Save contract.
// Read mode shows the profile facts, the groups the user belongs to (a drill to the
// group when the Users page is the root), the grants (read-only chips), and the
// impersonate actions. The header pencil opens edit mode: the profile becomes
// inputs, the grant builder goes live, and the footer reveals Disable / Enable;
// Save commits the profile and grant changes as one session. The lifecycle ladder
// runs Disable (reversible suspend) -> Archive (soft delete, reversible) -> Purge
// (permanent), the reversible toggle in the left slot and the escalating steps in
// the kebab.
export function UserDetail(props: { id: string }) {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const canUpdate = () => can(me.data, "principal", "update");
  const canArchive = () => can(me.data, "principal", "archive");
  const canPurge = () => can(me.data, "principal", "purge", "admin");
  const canResetPassword = () => can(me.data, "principal", "reset-password");
  // The admin reset-password panel (toggled from the kebab): its own new-password
  // field, a server-policy error routed inline, and a "set" confirmation.
  const [resetting, setResetting] = createSignal(false);
  const [resetPw, setResetPw] = createSignal("");
  const [resetBusy, setResetBusy] = createSignal(false);
  const [resetPwErr, setResetPwErr] = createSignal<string | null>(null);
  const [resetDone, setResetDone] = createSignal(false);
  // Fetch by id, not from the directory list (which hides archived principals),
  // so the blade still resolves a user it just soft-deleted and can offer Restore
  // / Purge. Mutations invalidate the PRINCIPALS_KEY prefix, covering this and the list.
  const principal = useQuery(() => ({ queryKey: [...PRINCIPALS_KEY, props.id], queryFn: () => getPrincipal(props.id) }));
  const p = () => principal.data ?? null;
  const refresh = () => qc.invalidateQueries({ queryKey: PRINCIPALS_KEY });
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
    // Gate the footer Save on the inline rules (the same handle/email rules the
    // server enforces), so an invalid username or email cannot be committed.
    valid: () => !handleError(username()) && !emailError(email()),
    // The left slot is the reversible state toggle for the lifecycle ladder: an
    // archived account restores (Restore), a live one toggles Disable / Enable.
    destructive: () => {
      const pr = p();
      if (!pr) return undefined;
      if (pr.archived_at) {
        return canArchive() ? { label: "Restore", tone: "ok", onClick: doRestore } : undefined;
      }
      if (!canUpdate()) return undefined;
      return pr.active
        ? { label: "Disable", tone: "warn", onClick: () => toggleActive(pr) }
        : { label: "Enable", tone: "ok", onClick: () => toggleActive(pr) };
    },
    // The kebab holds the escalating delete (Archive when live, Purge when
    // archived, both red and confirmed) and impersonate (not on self).
    secondary: () => {
      const pr = p();
      if (!pr) return [];
      const items: { label: string; icon?: JSX.Element; tone?: "danger"; onClick: () => void }[] = [];
      if (pr.archived_at) {
        if (canPurge()) items.push({ label: "Purge", icon: <Trash size={15} />, tone: "danger", onClick: doPurge });
      } else if (canArchive()) {
        items.push({ label: "Archive", icon: <Ban size={15} />, tone: "danger", onClick: doArchive });
      }
      if (pr.human && canResetPassword()) {
        items.push({ label: "Reset password", icon: <Key size={15} />, onClick: startReset });
      }
      if (can(me.data, "principal", "impersonate") && pr.id !== me.data?.principal?.id) {
        items.push({ label: "View as", icon: <Eye size={15} />, onClick: () => doImpersonate(pr, "view_as") });
        items.push({ label: "Act as", icon: <Mask size={15} />, onClick: () => doImpersonate(pr, "act_as") });
      }
      return items;
    },
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

  // A just-created user opens straight in edit mode (once its data has loaded and if
  // the caller can update it), so grants are assigned without a second step. Mirrors
  // the group create flow; the flag clears so this begins editing exactly once.
  createEffect(() => {
    if (principal.data && !edit.editing() && canUpdate() && consumePendingPrincipalEdit(props.id)) {
      edit.begin();
    }
  });

  async function toggleActive(pr: Principal) {
    setActErr(null);
    try {
      await setPrincipalActive(pr.id, !pr.active);
      await refresh();
    } catch (e) {
      setActErr(describeError(e));
    }
  }

  async function doArchive() {
    const pr = p();
    if (!pr || !confirm(`Archive "${principalName(pr)}"? They will be hidden and cannot sign in, reversibly. You can then purge them.`)) return;
    setActErr(null);
    try {
      await archivePrincipal(pr.id);
      // Archiving hides the user from the directory, so close its blade (like purge
      // and group delete). It stays restorable via the Show archived toggle.
      blades.close();
      await refresh();
    } catch (e) {
      setActErr(describeError(e));
    }
  }

  async function doRestore() {
    const pr = p();
    if (!pr) return;
    setActErr(null);
    try {
      await restorePrincipal(pr.id);
      await refresh();
    } catch (e) {
      setActErr(describeError(e));
    }
  }

  async function doPurge() {
    const pr = p();
    if (!pr || !confirm(`Purge "${principalName(pr)}"? This permanently deletes the account and its grants and memberships. It cannot be undone. The audit trail is kept.`)) return;
    setActErr(null);
    try {
      await purgePrincipal(pr.id);
      // The principal is gone, so close the blade first (this unmounts the body and
      // deactivates its detail query) and drop the dead detail cache, then refresh the
      // directory. Refreshing before closing would refetch the purged id and 404,
      // leaving the blade open on a broken state.
      blades.close();
      qc.removeQueries({ queryKey: [...PRINCIPALS_KEY, pr.id] });
      await refresh();
    } catch (e) {
      setActErr(describeError(e));
    }
  }

  async function doImpersonate(pr: Principal, mode: "view_as" | "act_as") {
    setActErr(null);
    const r = await impersonate(qc, pr.id, principalName(pr), mode);
    if (!r.ok) setActErr(r.message);
  }

  function startReset() {
    setResetPw("");
    setResetPwErr(null);
    setResetDone(false);
    setResetting(true);
  }

  async function doReset() {
    const pr = p();
    if (!pr) return;
    setResetBusy(true);
    setResetPwErr(null);
    setResetDone(false);
    try {
      await resetPrincipalPassword(pr.id, resetPw());
      setResetDone(true);
    } catch (e) {
      const msg = describeError(e);
      if (isPasswordPolicyMessage(msg)) setResetPwErr(msg);
      else setActErr(msg);
    } finally {
      setResetBusy(false);
    }
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
              <Show when={pr().archived_at} fallback={<Show when={!pr().active}><span class="badge badge-soft badge-warning badge-sm">inactive</span></Show>}>
                <span class="badge badge-soft badge-error badge-sm">archived</span>
              </Show>
            </span>
          </div>

          <Show when={actErr()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{actErr()}</span></div>
          </Show>

          {/* Admin reset-password panel (toggled from the kebab). The set password
              stays copyable so the admin can hand it over; the user changes it after. */}
          <Show when={resetting()}>
            <div class="flex flex-col gap-2 rounded-box border border-warning/40 bg-warning/10 p-3">
              <div class="flex items-center justify-between">
                <span class="eyebrow">Reset password{pr().human ? ` for ${pr().human!.username}` : ""}</span>
                <button type="button" class="btn btn-quiet btn-xs btn-square" aria-label="Close" onClick={() => setResetting(false)}><X size={14} /></button>
              </div>
              <PasswordField id="reset-pw" value={resetPw()} onInput={(v) => { setResetPw(v); setResetPwErr(null); setResetDone(false); }} username={pr().human?.username} serverError={resetPwErr()} generate />
              <Show when={resetDone()}>
                <p class="text-xs text-success">Password set. Copy it above and share it with the user; they can change it after signing in.</p>
              </Show>
              <div class="flex justify-end gap-2">
                <button type="button" class="btn btn-quiet btn-sm" onClick={() => setResetting(false)} disabled={resetBusy()}>Close</button>
                <button type="button" class="btn btn-action btn-sm" onClick={doReset} disabled={resetBusy() || !resetPw() || !!passwordError(resetPw(), pr().human?.username)}>
                  <Show when={resetBusy()}><span class="loading loading-spinner loading-xs" /></Show>
                  Set password
                </button>
              </div>
            </div>
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
                <input id="edit-username" autocomplete="off" class="input input-bordered w-full font-data" classList={{ "input-error": !!handleError(username()) }} value={username()} placeholder="jordan" onInput={(e) => setUsername(e.currentTarget.value)} />
                <Show when={handleError(username())}>{(msg) => <p class="mt-1 text-[11px] text-error">{msg()}</p>}</Show>
              </div>
              <div>
                <label class="eyebrow mb-1.5 block" for="edit-display">Display name</label>
                <input id="edit-display" autocomplete="off" class="input input-bordered w-full" value={displayName()} placeholder="Jordan Rivera" onInput={(e) => setDisplayName(e.currentTarget.value)} />
              </div>
              <div>
                <label class="eyebrow mb-1.5 block" for="edit-email">Email</label>
                <input id="edit-email" type="email" autocomplete="off" class="input input-bordered w-full" classList={{ "input-error": !!emailError(email()) }} value={email()} placeholder="jordan@example.com" onInput={(e) => setEmail(e.currentTarget.value)} />
                <Show when={emailError(email())}>{(msg) => <p class="mt-1 text-[11px] text-error">{msg()}</p>}</Show>
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
  const principal = useQuery(() => ({ queryKey: [...PRINCIPALS_KEY, props.id], queryFn: () => getPrincipal(props.id) }));
  return <>{principal.data ? principalName(principal.data) : "User"}</>;
}

export const userBlade: BladeDef = {
  Title: (p) => <UserTitle id={p.id} />,
  Body: (p) => <UserDetail id={p.id} />,
};
