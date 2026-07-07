import { For, Show, createMemo, createSignal } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Page from "../components/Page";
import TreeSelect from "../components/TreeSelect";
import type { TreeNode } from "../lib/treeselect";
import { type Principal, type Grant, type ScopeKind, PRINCIPALS_KEY, ROLES_KEY, listPrincipals, createPrincipal, updatePrincipal, createGrant, revokeGrant, setPrincipalActive, listRoles, principalName } from "../lib/principals";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { listLocations } from "../lib/locations";
import { listSystems } from "../lib/systems";
import { listComponents } from "../lib/components";
import { Plus } from "../components/icons";

// Users: the admin principal directory. A read grid of every principal (human or
// service account) with its role grants, a detail panel for the selected one, and
// a create form for a new human (gated by principal:create). It is self-teaching:
// the detail panel shows the grant model (role x scope) the platform enforces.
const kindBadge = (kind: string) => `badge badge-soft badge-sm capitalize ${kind === "service" ? "badge-info" : "badge-primary"}`;

export default function Users() {
  const qc = useQueryClient();
  const me = useMe();
  const principals = useQuery(() => ({ queryKey: PRINCIPALS_KEY, queryFn: () => listPrincipals() }));

  const [selectedId, setSelectedId] = createSignal<string | null>(null);
  const selected = createMemo(() => principals.data?.find((p) => p.id === selectedId()) ?? null);
  const [createOpen, setCreateOpen] = createSignal(false);
  const [editOpen, setEditOpen] = createSignal(false);
  const [actErr, setActErr] = createSignal<string | null>(null);

  async function toggleActive(p: Principal) {
    setActErr(null);
    try {
      await setPrincipalActive(p.id, !p.active);
      await qc.invalidateQueries({ queryKey: PRINCIPALS_KEY });
    } catch (e) {
      setActErr(describeError(e));
    }
  }

  const initials = (p: Principal) => principalName(p).slice(0, 2).toUpperCase();

  return (
    <Page title="Users">
      <div class="mb-4 flex items-center gap-3">
        <p class="text-sm text-base-content/60">Every principal that can authenticate: operators and service accounts, with the roles granted to each.</p>
        <span class="flex-1" />
        <Show when={can(me.data, "principal", "create")}>
          <button class="btn btn-action btn-sm gap-1.5" onClick={() => setCreateOpen(true)}><Plus size={14} /> New user</button>
        </Show>
      </div>

      <Show when={principals.error}>
        <div role="alert" class="alert alert-error alert-soft mb-4 text-sm"><span>{describeError(principals.error)}</span></div>
      </Show>

      <div class="grid gap-4 lg:grid-cols-[1.4fr_1fr]">
        {/* Directory grid */}
        <div class="overflow-hidden rounded-box border border-base-300">
          <table class="table table-sm">
            <thead>
              <tr>
                <th>Name</th>
                <th>Kind</th>
                <th>Grants</th>
              </tr>
            </thead>
            <tbody>
              <For each={principals.data ?? []} fallback={<tr><td colspan="3" class="text-center text-base-content/40">{principals.isLoading ? "Loading…" : "No principals"}</td></tr>}>
                {(p) => (
                  <tr
                    class="cursor-pointer hover:bg-base-content/5"
                    classList={{ "bg-primary/10": p.id === selectedId() }}
                    onClick={() => setSelectedId(p.id)}
                  >
                    <td>
                      <div class="flex items-center gap-2.5">
                        <div class="avatar avatar-placeholder">
                          <div class="w-7 rounded-full bg-linear-to-br from-primary to-info text-primary-content">
                            <span class="font-data text-[10px] font-bold uppercase">{initials(p)}</span>
                          </div>
                        </div>
                        <div class="min-w-0 leading-tight">
                          <div class="truncate text-sm font-medium">{principalName(p)}</div>
                          <Show when={p.human}><div class="truncate font-data text-[11px] text-base-content/40">{p.human!.username}</div></Show>
                        </div>
                      </div>
                    </td>
                    <td>
                      <span class={kindBadge(p.kind)}>{p.kind}</span>
                      <Show when={!p.active}><span class="badge badge-soft badge-warning badge-sm ml-1">inactive</span></Show>
                    </td>
                    <td class="tnum text-base-content/60">{p.grants.length}</td>
                  </tr>
                )}
              </For>
            </tbody>
          </table>
        </div>

        {/* Detail panel */}
        <div class="card h-fit border border-base-300 bg-base-200">
          <div class="card-body gap-3">
            <Show when={selected()} fallback={<p class="py-8 text-center text-sm text-base-content/40">Select a user to see its profile and grants.</p>}>
              {(p) => (
                <>
                  <div class="flex items-center gap-3">
                    <div class="avatar avatar-placeholder">
                      <div class="w-12 rounded-full bg-linear-to-br from-primary to-info text-primary-content">
                        <span class="font-data text-sm font-bold uppercase">{initials(p())}</span>
                      </div>
                    </div>
                    <div class="min-w-0 flex-1">
                      <div class="truncate text-base font-semibold">{principalName(p())}</div>
                      <span class="flex items-center gap-1.5">
                        <span class={kindBadge(p().kind)}>{p().kind}</span>
                        <Show when={!p().active}><span class="badge badge-soft badge-warning badge-sm">inactive</span></Show>
                      </span>
                    </div>
                  </div>
                  <Show when={actErr()}>
                    <div role="alert" class="alert alert-error alert-soft text-sm"><span>{actErr()}</span></div>
                  </Show>
                  <div class="grid grid-cols-2 gap-3 text-sm">
                    <Show when={p().human}>
                      <Fact label="Username" value={<span class="font-data">{p().human!.username}</span>} />
                      <Fact label="Email" value={p().human!.email || <span class="text-base-content/40">not set</span>} />
                    </Show>
                    <Show when={p().service}>
                      <Fact label="Label" value={<span class="font-data">{p().service!.label}</span>} />
                    </Show>
                  </div>
                  <GrantEditor
                    principal={p()}
                    canGrant={can(me.data, "principal_grant", "create")}
                    canRevoke={can(me.data, "principal_grant", "delete")}
                    onChange={() => qc.invalidateQueries({ queryKey: PRINCIPALS_KEY })}
                  />
                  <Show when={can(me.data, "principal", "update")}>
                    <div class="flex items-center gap-2 border-t border-base-300 pt-3">
                      <button
                        class="btn btn-sm"
                        classList={{ "btn-warn": p().active, "btn-ok": !p().active }}
                        onClick={() => toggleActive(p())}
                      >
                        {p().active ? "Disable" : "Enable"}
                      </button>
                      <span class="flex-1" />
                      <Show when={p().human}>
                        <button class="btn btn-action btn-sm" onClick={() => setEditOpen(true)}>Edit</button>
                      </Show>
                    </div>
                  </Show>
                </>
              )}
            </Show>
          </div>
        </div>
      </div>

      <Show when={createOpen()}>
        <CreateUserModal
          close={() => setCreateOpen(false)}
          onCreated={async (p) => {
            await qc.invalidateQueries({ queryKey: PRINCIPALS_KEY });
            setSelectedId(p.id);
            setCreateOpen(false);
          }}
        />
      </Show>

      <Show when={editOpen() && selected()?.human}>
        <EditUserModal
          principal={selected()!}
          close={() => setEditOpen(false)}
          onSaved={async () => {
            await qc.invalidateQueries({ queryKey: PRINCIPALS_KEY });
            setEditOpen(false);
          }}
        />
      </Show>
    </Page>
  );
}

function Fact(props: { label: string; value: unknown }) {
  return (
    <div>
      <div class="eyebrow">{props.label}</div>
      <div>{props.value as never}</div>
    </div>
  );
}

// SCOPE_KINDS the grant form offers. "group" is deferred (no group entity yet), so
// it is not offered; the server also rejects it.
const SCOPE_KINDS: Exclude<ScopeKind, "group">[] = ["all", "location", "system", "component"];

// GrantEditor shows a principal's role grants (each a role at a scope), lets an
// admin revoke one (the x, gated principal_grant:delete) and add one (the form,
// gated principal_grant:create). A scoped grant targets a real entity by id: the
// form is a picker of the chosen kind (value = id, label = name), and the chips
// resolve the stored id back to its name. The server enforces the owner invariant
// (the last owner grant cannot be revoked) and answers 409.
function GrantEditor(props: { principal: Principal; canGrant: boolean; canRevoke: boolean; onChange: () => void | Promise<void> }) {
  const roles = useQuery(() => ({ queryKey: ROLES_KEY, queryFn: listRoles, enabled: props.canGrant }));
  const locations = useQuery(() => ({ queryKey: ["locations"], queryFn: listLocations, enabled: props.canGrant }));
  const systems = useQuery(() => ({ queryKey: ["systems"], queryFn: listSystems, enabled: props.canGrant }));
  const components = useQuery(() => ({ queryKey: ["components"], queryFn: listComponents, enabled: props.canGrant }));

  // id -> name across the tree tiers, for turning a stored scope id back into a
  // readable grant chip.
  const nameOf = createMemo(() => {
    const m = new Map<string, string>();
    for (const e of locations.data ?? []) m.set(e.id, e.name);
    for (const e of systems.data ?? []) m.set(e.id, e.name);
    for (const e of components.data ?? []) m.set(e.id, e.name);
    return m;
  });
  const label = (g: Grant) => {
    if (g.scope_kind === "all") return `${g.role} @ all`;
    const name = g.scope_id ? nameOf().get(g.scope_id) ?? g.scope_id : g.scope_id;
    return `${g.role} @ ${g.scope_kind}:${name}`;
  };
  // The scope entities of the chosen kind, as TreeNodes so the picker reads as an
  // indented tree (value = id, ordered by the parent_id tree) rather than a flat
  // alphabetical list.
  const scopeItems = (kind: string): TreeNode[] => {
    const list = kind === "location" ? locations.data ?? [] : kind === "system" ? systems.data ?? [] : kind === "component" ? components.data ?? [] : [];
    return list.map((e) => ({ id: e.id, value: e.id, label: e.name, parentId: e.parent_id, rank: 0 }));
  };

  const [role, setRole] = createSignal("");
  const [scopeKind, setScopeKind] = createSignal<Exclude<ScopeKind, "group">>("all");
  const [scopeId, setScopeId] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  async function add(e: SubmitEvent) {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      await createGrant(props.principal.id, {
        role: role(),
        scope_kind: scopeKind(),
        scope_id: scopeKind() === "all" ? undefined : scopeId() || undefined,
      });
      setRole("");
      setScopeKind("all");
      setScopeId("");
      await props.onChange();
    } catch (er) {
      setErr(describeError(er));
    } finally {
      setBusy(false);
    }
  }
  async function remove(g: Grant) {
    if (!g.id) return;
    setErr(null);
    try {
      await revokeGrant(props.principal.id, g.id);
      await props.onChange();
    } catch (er) {
      setErr(describeError(er));
    }
  }

  return (
    <div>
      <div class="eyebrow mb-1.5">Role grants</div>
      <div class="flex flex-wrap gap-1.5">
        <For each={props.principal.grants} fallback={<span class="text-xs text-base-content/40">No grants yet. This principal can sign in but has no permissions.</span>}>
          {(g) => (
            <span class="badge badge-soft badge-primary gap-1 font-data text-[11px]">
              {label(g)}
              <Show when={props.canRevoke && g.id}>
                <button type="button" class="leading-none hover:text-error" title="Revoke" aria-label={`Revoke ${label(g)}`} onClick={() => remove(g)}>&times;</button>
              </Show>
            </span>
          )}
        </For>
      </div>
      <Show when={err()}>
        <p class="mt-2 text-[11px] text-error">{err()}</p>
      </Show>
      <Show when={props.canGrant}>
        <form class="mt-3 flex flex-wrap items-end gap-2" onSubmit={add}>
          <select class="select select-bordered select-sm" value={role()} onChange={(e) => setRole(e.currentTarget.value)} required aria-label="Role">
            <option value="" disabled>Role…</option>
            <For each={roles.data}>{(r) => <option value={r.id}>{r.id}</option>}</For>
          </select>
          <select
            class="select select-bordered select-sm"
            value={scopeKind()}
            onChange={(e) => { setScopeKind(e.currentTarget.value as Exclude<ScopeKind, "group">); setScopeId(""); }}
            aria-label="Scope kind"
          >
            <For each={SCOPE_KINDS}>{(k) => <option value={k}>{k}</option>}</For>
          </select>
          <Show when={scopeKind() !== "all"}>
            <TreeSelect
              class="select select-bordered select-sm"
              items={scopeItems(scopeKind())}
              value={scopeId()}
              onChange={setScopeId}
              rootLabel={`Select a ${scopeKind()}…`}
            />
          </Show>
          <button type="submit" class="btn btn-action btn-sm" disabled={busy() || !role() || (scopeKind() !== "all" && !scopeId())}>Grant</button>
        </form>
      </Show>
    </div>
  );
}

// CreateUserModal is the new-human form: username (required), display name, email,
// and an optional initial password (min 8) the user changes after signing in.
function CreateUserModal(props: { close: () => void; onCreated: (p: Principal) => void | Promise<void> }) {
  const [username, setUsername] = createSignal("");
  const [displayName, setDisplayName] = createSignal("");
  const [email, setEmail] = createSignal("");
  const [password, setPassword] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      const created = await createPrincipal({
        username: username().trim(),
        display_name: displayName().trim() || undefined,
        email: email().trim() || undefined,
        password: password() || undefined,
      });
      await props.onCreated(created);
    } catch (er) {
      setErr(describeError(er));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div class="modal modal-open">
      <div class="modal-box">
        <h3 class="text-base font-semibold">New user</h3>
        <p class="mb-3 mt-1 text-xs text-base-content/50">Creates a human principal. Assign roles afterwards; a user with no grants can sign in but has no permissions.</p>
        <form class="flex flex-col gap-3" onSubmit={submit}>
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div>
            <label class="eyebrow mb-1.5 block" for="new-username">Username</label>
            <input id="new-username" autocomplete="off" class="input input-bordered w-full font-data" value={username()} placeholder="jordan" onInput={(e) => setUsername(e.currentTarget.value)} disabled={busy()} required />
          </div>
          <div>
            <label class="eyebrow mb-1.5 block" for="new-display">Display name</label>
            <input id="new-display" autocomplete="off" class="input input-bordered w-full" value={displayName()} placeholder="Jordan Rivera" onInput={(e) => setDisplayName(e.currentTarget.value)} disabled={busy()} />
          </div>
          <div>
            <label class="eyebrow mb-1.5 block" for="new-email">Email</label>
            <input id="new-email" type="email" autocomplete="off" class="input input-bordered w-full" value={email()} onInput={(e) => setEmail(e.currentTarget.value)} disabled={busy()} />
          </div>
          <div>
            <label class="eyebrow mb-1.5 block" for="new-password">Initial password</label>
            <input id="new-password" type="password" autocomplete="new-password" minLength={8} class="input input-bordered w-full" value={password()} onInput={(e) => setPassword(e.currentTarget.value)} disabled={busy()} />
            <p class="mt-1 text-[11px] text-base-content/40">Optional, at least 8 characters. The user changes it after signing in.</p>
          </div>
          <div class="mt-1 flex justify-end gap-2">
            <button type="button" class="btn btn-quiet btn-sm" onClick={props.close} disabled={busy()}>Cancel</button>
            <button type="submit" class="btn btn-action btn-sm" disabled={busy() || !username().trim()}>
              <Show when={busy()}><span class="loading loading-spinner loading-xs" /></Show>
              Create user
            </button>
          </div>
        </form>
      </div>
      <button class="modal-backdrop" onClick={props.close} aria-label="Close" />
    </div>
  );
}

// EditUserModal edits a human principal's admin-owned fields: display name, email,
// and username. Username is renamable here (it is not a key); the user cannot edit
// it themselves. Only the changed fields are sent.
function EditUserModal(props: { principal: Principal; close: () => void; onSaved: () => void | Promise<void> }) {
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
      // Send only the fields that changed.
      const patch: { username?: string; display_name?: string; email?: string } = {};
      if (username().trim() !== h.username) patch.username = username().trim();
      if (displayName().trim() !== (h.display_name ?? "")) patch.display_name = displayName().trim();
      if (email().trim() !== (h.email ?? "")) patch.email = email().trim();
      await updatePrincipal(props.principal.id, patch);
      await props.onSaved();
    } catch (er) {
      setErr(describeError(er));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div class="modal modal-open">
      <div class="modal-box">
        <h3 class="text-base font-semibold">Edit user</h3>
        <p class="mb-3 mt-1 text-xs text-base-content/50">Change this user's display name, email, or username. Renaming is safe: their credentials and grants follow the account.</p>
        <form class="flex flex-col gap-3" onSubmit={submit}>
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
            <button type="button" class="btn btn-quiet btn-sm" onClick={props.close} disabled={busy()}>Cancel</button>
            <button type="submit" class="btn btn-action btn-sm" disabled={busy() || !username().trim()}>
              <Show when={busy()}><span class="loading loading-spinner loading-xs" /></Show>
              Save changes
            </button>
          </div>
        </form>
      </div>
      <button class="modal-backdrop" onClick={props.close} aria-label="Close" />
    </div>
  );
}
