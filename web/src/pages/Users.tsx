import { For, Show, createMemo, createSignal } from "solid-js";
import { useNavigate } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import GrantBuilder from "../components/GrantBuilder";
import type { TreeNode } from "../lib/treeselect";
import type { ExistingGrant, GrantRef, ScopeOp } from "../lib/grantdraft";
import { type Principal, type ScopeKind, type UpdatePrincipal, PRINCIPALS_KEY, ROLES_KEY, listPrincipals, createPrincipal, updatePrincipal, createGrant, revokeGrant, setPrincipalActive, listRoles, principalName } from "../lib/principals";
import { useMe, can } from "../lib/auth";
import { impersonate } from "../lib/impersonation";
import { describeError } from "../lib/format";
import { listLocations } from "../lib/locations";
import { listSystems } from "../lib/systems";
import { listComponents } from "../lib/components";
import type { FilterKey } from "../lib/predicate";

// Users: the admin principal directory, now a config over the shared FlatList (the
// flat sibling of the inventory TreeList, both wearing ListShell's chrome). A row
// per principal (human or service account) with its role grants; a row opens the
// side Drawer detail (profile facts, the grant builder, impersonate, disable /
// enable, and an inline edit), and "New user" opens the create Drawer (gated by
// principal:create). It is self-teaching: the detail shows the grant model (role x
// scope) the platform enforces. Every gate is a UI hint; the server is authority.
const kindBadge = (kind: string) => `badge badge-soft badge-sm capitalize ${kind === "service" ? "badge-info" : "badge-primary"}`;
const initials = (p: Principal) => principalName(p).slice(0, 2).toUpperCase();

// The faceted-search fields, matching the FilterBar contract the audit trail and
// inventory lists use: name (substring default), kind (an exact human/service
// facet), and username (substring). Matching is client-side over the loaded rows.
const filterKeys: FilterKey<Principal>[] = [
  { key: "name", type: "string", hint: "substring", get: (p) => principalName(p) },
  { key: "kind", type: "string", hint: "exact", get: (p) => p.kind, values: (rows) => [...new Set(rows.map((r) => r.kind))].sort() },
  { key: "username", type: "string", hint: "substring", get: (p) => p.human?.username ?? "" },
];

// The directory columns. Name carries the avatar initials + display name + username;
// Kind the human/service badge (with an inactive marker); Grants the grant count.
const columns: FlatColumn<Principal>[] = [
  {
    key: "name", label: "Name", sortVal: (p) => principalName(p).toLowerCase(),
    cell: (p) => (
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
    ),
  },
  {
    key: "kind", label: "Kind", width: "160px", sortVal: (p) => p.kind,
    cell: (p) => (
      <>
        <span class={kindBadge(p.kind)}>{p.kind}</span>
        <Show when={!p.active}><span class="badge badge-soft badge-warning badge-sm ml-1">inactive</span></Show>
      </>
    ),
  },
  {
    key: "grants", label: "Grants", width: "100px", sortVal: (p) => p.grants.length,
    cell: (p) => <span class="tnum text-base-content/60">{p.grants.length}</span>,
  },
  {
    key: "groups", label: "Groups",
    cell: (p) => (
      <Show when={p.groups?.length} fallback={<span class="text-base-content/30">—</span>}>
        <span class="inline-flex flex-wrap gap-1">
          <For each={p.groups}>{(g) => <span class="badge badge-ghost badge-sm">{g.name}</span>}</For>
        </span>
      </Show>
    ),
  },
];

export default function Users() {
  const me = useMe();
  const principals = useQuery(() => ({ queryKey: PRINCIPALS_KEY, queryFn: () => listPrincipals() }));

  return (
    <FlatList<Principal>
      config={{
        entity: { name: "user", plural: "users" },
        rows: () => principals.data ?? [],
        loading: () => principals.isLoading,
        error: () => principals.error,
        filterKeys,
        filterPlaceholder: "filter by name, kind, username",
        columns,
        empty: "No users yet.",
        detail: (p) => ({ title: principalName(p), body: <UserDetail id={p.id} /> }),
        create: {
          label: "New user",
          can: () => can(me.data, "principal", "create"),
          body: (ctx) => <CreateUserForm close={ctx.close} onCreated={ctx.select} />,
        },
      }}
    />
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

// UserDetail is the row's side-Drawer body. It re-derives the principal from the
// live query by id (not the row snapshot the Drawer opened with), so a disable /
// enable or an inline edit reflects immediately after the query invalidates. The
// read view carries the profile facts, the grant builder, disable / enable, edit,
// and impersonate; the Edit button swaps the body to the inline edit form in place
// (no nested dialog) and returns to the read view on cancel or save.
function UserDetail(props: { id: string }) {
  const qc = useQueryClient();
  const me = useMe();
  const principals = useQuery(() => ({ queryKey: PRINCIPALS_KEY, queryFn: () => listPrincipals() }));
  const p = createMemo(() => principals.data?.find((x) => x.id === props.id) ?? null);
  const [editing, setEditing] = createSignal(false);
  const [actErr, setActErr] = createSignal<string | null>(null);

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
        <div class="flex flex-col gap-3">
          <div class="flex items-center gap-3">
            <div class="avatar avatar-placeholder">
              <div class="w-12 rounded-full bg-linear-to-br from-primary to-info text-primary-content">
                <span class="font-data text-sm font-bold uppercase">{initials(pr())}</span>
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
                <GrantEditor
                  principal={pr()}
                  canGrant={can(me.data, "principal_grant", "create")}
                  canRevoke={can(me.data, "principal_grant", "delete")}
                  onChange={() => qc.invalidateQueries({ queryKey: PRINCIPALS_KEY })}
                />
                <Show when={can(me.data, "principal", "update")}>
                  <div class="flex items-center gap-2 border-t border-base-300 pt-3">
                    <button
                      class="btn btn-sm"
                      classList={{ "btn-warn": pr().active, "btn-ok": !pr().active }}
                      onClick={() => toggleActive(pr())}
                    >
                      {pr().active ? "Disable" : "Enable"}
                    </button>
                    <span class="flex-1" />
                    <Show when={pr().human}>
                      <button class="btn btn-action btn-sm" onClick={() => setEditing(true)}>Edit</button>
                    </Show>
                  </div>
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
  const navigate = useNavigate();
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
      // Resync the current set regardless of outcome, so a partial batch reflects
      // what actually persisted.
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
          changed by editing the group, not the member. */}
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
                  <button class="text-primary hover:underline" title="Open this group" onClick={() => navigate(`/groups?g=${g.group_id}`)}>{g.group_name}</button>
                </span>
              )}
            </For>
          </div>
        </div>
      </Show>
    </div>
  );
}

// CreateUserForm is the new-human form the create Drawer hosts: username (required),
// display name, email, and an optional initial password (min 8) the user changes
// after signing in. On success it invalidates the directory and hands the created
// principal to onCreated, which opens its detail Drawer (closing this one), so the
// operator lands straight on the new user (grants next).
function CreateUserForm(props: { close: () => void; onCreated: (p: Principal) => void }) {
  const qc = useQueryClient();
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
      // Invalidate first so the directory (and the detail Drawer's re-derive by id)
      // has the new row before we select it, then land on it.
      await qc.invalidateQueries({ queryKey: PRINCIPALS_KEY });
      props.onCreated(created);
    } catch (er) {
      setErr(describeError(er));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form class="flex flex-col gap-3" onSubmit={submit}>
      <p class="text-xs text-base-content/50">Creates a human principal. Assign roles afterwards; a user with no grants can sign in but has no permissions.</p>
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
  );
}

// EditForm edits a human principal's admin-owned fields inline in the detail Drawer
// (no nested dialog): display name, email, and username. Username is renamable on
// this admin page (it is not a key, and the whole edit is gated by principal:update,
// the platform's admin capability over principals; a user cannot rename themselves
// from the self-service profile). Only the changed fields are sent; on save it
// invalidates the directory and returns to the read view.
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
      // Send only the fields that changed.
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
