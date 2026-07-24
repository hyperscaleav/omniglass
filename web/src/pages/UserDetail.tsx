import { type JSX, For, Show, createEffect, createMemo, createResource, createSignal, on } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import GrantBuilder from "../components/GrantBuilder";
import { RelatedList } from "../components/DetailShell";
import KVStacked from "../components/KVStacked";
import { Ban, Eye, Key, LogOut, Mask, Trash, X } from "../components/icons";
import PasswordField from "../components/PasswordField";
import Button from "../components/Button";
import SessionsList from "../components/SessionsList";
import { usePrincipalSessions, useRevokePrincipalSession, useRevokeAllPrincipalSessions, type Session } from "../lib/sessions";
import { useBlades, useBladeEdit } from "../lib/blades";
import type { BladeDef } from "../lib/blades";
import type { TreeNode } from "../lib/treeselect";
import type { ExistingGrant, GrantRef, ScopeOp } from "../lib/grantdraft";
import {
  type Principal, type ScopeKind, type UpdatePrincipal,
  PRINCIPALS_KEY, ROLES_KEY, getPrincipal, updatePrincipal, createGrant, revokeGrant, setPrincipalActive, listRoles,
  archivePrincipal, restorePrincipal, purgePrincipal, resetPrincipalPassword, consumePendingPrincipalEdit,
  setPrincipalAvatar, removePrincipalAvatar, principalAvatarUrl,
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
  const canSetAvatar = () => can(me.data, "principal", "set-avatar");
  const canRevokeSession = () => can(me.data, "principal", "revoke-session");
  const revokeAllSessions = useRevokeAllPrincipalSessions(props.id);
  // An owner's credentials cannot be revoked by anyone (the takeover guard, server-side).
  // Detect it from the target's grants so the console hides the revoke affordances rather
  // than offering an action that would 403: an admin can see an owner's sessions, not end
  // them. The capability-escalation half of the guard is rarer and still errors gracefully.
  const isOwnerTarget = () => (p()?.grants ?? []).some((g) => g.role === "owner");
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

  // The profile picture, fetched as a data URL only when the principal has one
  // (has_avatar rides on the detail read, so a pictureless user fires no request).
  // Shown in the header, and (for an admin who can set it) managed in the edit blade.
  const [avatarUrl, { refetch: refetchAvatar }] = createResource(
    () => p()?.human?.has_avatar ?? false,
    (has) => (has ? principalAvatarUrl(props.id) : Promise.resolve(null)),
  );
  let avatarFileInput: HTMLInputElement | undefined;

  async function onPickAvatar(e: Event) {
    const pr = p();
    const file = (e.currentTarget as HTMLInputElement).files?.[0];
    if (!pr || !file) return;
    setActErr(null);
    try {
      await setPrincipalAvatar(pr.id, file);
      await refresh(); // refresh has_avatar so the flag and image agree
      await refetchAvatar();
    } catch (er) {
      setActErr(describeError(er));
    }
  }

  async function onRemoveAvatar() {
    const pr = p();
    if (!pr) return;
    setActErr(null);
    try {
      await removePrincipalAvatar(pr.id);
      await refresh();
      await refetchAvatar();
    } catch (er) {
      setActErr(describeError(er));
    }
  }

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
      if (pr.human && canResetPassword() && pr.id !== me.data?.principal?.id) {
        items.push({ label: "Reset password", icon: <Key size={15} />, onClick: startReset });
      }
      if (can(me.data, "principal", "impersonate") && pr.id !== me.data?.principal?.id) {
        items.push({ label: "View as", icon: <Eye size={15} />, onClick: () => doImpersonate(pr, "view_as") });
        items.push({ label: "Act as", icon: <Mask size={15} />, onClick: () => doImpersonate(pr, "act_as") });
      }
      // Bulk session/token revoke, alongside the per-row Revoke in the Sessions section
      // and gated by the same capability: end every session or every token at once. Hidden
      // on an owner target, whose credentials the takeover guard makes un-revocable.
      if (canRevokeSession() && !isOwnerTarget()) {
        items.push({ label: "Revoke all sessions", icon: <LogOut size={15} />, tone: "danger", onClick: () => doRevokeAll("session") });
        items.push({ label: "Revoke all tokens", icon: <Trash size={15} />, tone: "danger", onClick: () => doRevokeAll("token") });
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

  // Bulk-revoke every one of the target's sessions or tokens from the kebab. Confirmed
  // (it signs the user out of all of that kind at once), scoped to purpose so one never
  // touches the other, then the Sessions section refetches from the invalidated query.
  async function doRevokeAll(purpose: "session" | "token") {
    const noun = purpose === "session" ? "sessions" : "API tokens";
    if (!confirm(`Revoke all ${noun} for this user? They are signed out of every ${purpose} at once.`)) return;
    setActErr(null);
    const r = await revokeAllSessions(purpose);
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
            <Show
              when={avatarUrl()}
              fallback={
                <div class="avatar avatar-placeholder">
                  <div class="w-12 rounded-full bg-linear-to-br from-primary to-info text-primary-content">
                    <span class="font-data text-sm font-bold uppercase">{principalInitials(pr())}</span>
                  </div>
                </div>
              }
            >
              <div class="avatar">
                <div class="w-12 rounded-full">
                  <img src={avatarUrl()!} alt={principalName(pr())} />
                </div>
              </div>
            </Show>
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
                <Button square size="xs" icon={X} label="Close" onClick={() => setResetting(false)} />
              </div>
              <PasswordField id="reset-pw" value={resetPw()} onInput={(v) => { setResetPw(v); setResetPwErr(null); setResetDone(false); }} username={pr().human?.username} serverError={resetPwErr()} generate />
              <Show when={resetDone()}>
                <p class="text-xs text-success">Password set. Copy it above and share it with the user; they can change it after signing in.</p>
              </Show>
              <div class="flex justify-end gap-2">
                <Button onClick={() => setResetting(false)} disabled={resetBusy()}>Close</Button>
                <Button intent="action" onClick={doReset} loading={resetBusy()} disabled={!resetPw() || !!passwordError(resetPw(), pr().human?.username)}>Set password</Button>
              </div>
            </div>
          </Show>

          {/* Profile: facts in read mode, inputs in edit mode. */}
          <Show
            when={editing() && pr().human}
            fallback={
              <div class="grid grid-cols-2 gap-3 text-sm">
                <Show when={pr().human}>
                  <KVStacked label="Username" value={<span class="font-data">{pr().human!.username}</span>} />
                  <KVStacked label="Email" value={pr().human!.email || <span class="text-base-content/40">not set</span>} />
                </Show>
                <Show when={pr().service}>
                  <KVStacked label="Label" value={<span class="font-data">{pr().service!.label}</span>} />
                </Show>
              </div>
            }
          >
            <div class="flex flex-col gap-3">
              <Show when={canSetAvatar()}>
                <div>
                  <label class="eyebrow mb-1.5 block">Profile picture</label>
                  <div class="flex items-center gap-3">
                    <Show
                      when={avatarUrl()}
                      fallback={
                        <div class="avatar avatar-placeholder">
                          <div class="w-16 rounded-full bg-linear-to-br from-primary to-info text-primary-content">
                            <span class="font-data text-lg font-bold uppercase">{principalInitials(pr())}</span>
                          </div>
                        </div>
                      }
                    >
                      <div class="avatar">
                        <div class="w-16 rounded-full">
                          <img src={avatarUrl()!} alt={principalName(pr())} />
                        </div>
                      </div>
                    </Show>
                    <div class="flex flex-col gap-1">
                      <input
                        type="file"
                        accept="image/jpeg,image/png,image/webp"
                        class="hidden"
                        ref={avatarFileInput}
                        onChange={onPickAvatar}
                      />
                      <Button size="sm" onClick={() => avatarFileInput?.click()}>Upload</Button>
                      <Show when={pr().human?.has_avatar}>
                        <Button size="sm" intent="danger" onClick={onRemoveAvatar}>Remove</Button>
                      </Show>
                    </div>
                  </div>
                </div>
              </Show>
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

          {/* Sessions: the target's active sign-ins and API tokens, each revocable.
              Only rendered when the caller holds principal:revoke-session, so the
              affordance is hidden from an operator (a UI hint; the server is the
              authority, and it bounds every revoke to this principal). */}
          <Show when={canRevokeSession()}>
            <SessionsSection id={pr().id} revocable={!isOwnerTarget()} />
          </Show>
        </div>
      )}
    </Show>
  );
}

// SessionsSection lists a target principal's active sessions and tokens on the admin
// blade, each with a Revoke. It reuses the same SessionsList presentation as the
// self-service Profile card; the server bounds every read and revoke to this
// principal and never returns the secret. current is always false here (there is no
// "this request's own session" when viewing another principal), so every row reads
// as "Revoke". Rendered only when the caller holds principal:revoke-session. When the
// target is not revocable (an owner, per the takeover guard), the list stays read-only:
// the caller can see where the account is signed in but not end any of it.
function SessionsSection(props: { id: string; revocable: boolean }) {
  const sessions = usePrincipalSessions(props.id);
  const revokeSession = useRevokePrincipalSession(props.id);
  const [revoking, setRevoking] = createSignal<string | null>(null);
  const [err, setErr] = createSignal<string | null>(null);
  // Split by kind so sessions and API tokens each render in their own section, matching
  // the self-service Profile card. current is always false here (viewing another
  // principal), so every row reads as "Revoke".
  const sessionRows = () => (sessions.data ?? []).filter((s) => s.kind === "session");
  const tokenRows = () => (sessions.data ?? []).filter((s) => s.kind === "token");
  async function revoke(s: Session) {
    if (!confirm("Revoke this credential? The user is signed out of it immediately.")) return;
    setRevoking(s.id);
    setErr(null);
    const r = await revokeSession(s.id);
    if (!r.ok) setErr(r.message);
    setRevoking(null);
  }
  return (
    <div class="flex flex-col gap-4">
      <Show when={sessions.error}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>Could not load this user's sessions.</span></div>
      </Show>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <Show when={!props.revocable}>
        <p class="text-xs text-base-content/40">An owner's sessions and tokens cannot be revoked. You can see where the account is signed in, but not end it.</p>
      </Show>
      <div class="flex flex-col gap-2">
        <div class="eyebrow">Sessions</div>
        <p class="text-xs text-base-content/50">
          Where this account is signed in. Revoke a <span class="font-data text-base-content/70">session</span> to sign
          it out at once. The credential secret is never shown, only its <span class="font-data text-base-content/70">ogp_</span> locator.
        </p>
        <SessionsList sessions={sessionRows()} revoking={revoking()} onRevoke={props.revocable ? revoke : undefined} emptyLabel="No active sessions." />
      </div>
      <div class="flex flex-col gap-2">
        <div class="eyebrow">API tokens</div>
        <p class="text-xs text-base-content/50">
          Tokens this account minted for the CLI or API. Revoke any that should no longer work. The token secret is never
          shown, only its <span class="font-data text-base-content/70">ogp_</span> locator.
        </p>
        <SessionsList sessions={tokenRows()} revoking={revoking()} onRevoke={props.revocable ? revoke : undefined} emptyLabel="No API tokens." />
      </div>
    </div>
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
    return list.map((e) => ({ id: e.id, value: e.id, label: e.name, parentId: e.parent, rank: 0 }));
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
