import { For, Show, createMemo, createSignal } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import GrantBuilder from "../components/GrantBuilder";
import type { FilterKey } from "../lib/predicate";
import type { TreeNode } from "../lib/treeselect";
import type { ExistingGrant, GrantRef, ScopeOp } from "../lib/grantdraft";
import {
  type Group, GROUPS_KEY, groupName, memberName,
  listGroups, getGroup, createGroup, deleteGroup,
  listGroupMembers, addGroupMember, removeGroupMember,
  listGroupGrants, createGroupGrant, revokeGroupGrant,
} from "../lib/groups";
import { type ScopeKind, PRINCIPALS_KEY, ROLES_KEY, listPrincipals, listRoles, principalName } from "../lib/principals";
import { listLocations } from "../lib/locations";
import { listSystems } from "../lib/systems";
import { listComponents } from "../lib/components";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { Plus, Trash } from "../components/icons";

// Groups: the principal-group admin surface, a config over the shared FlatList. A
// group holds role x scope grants that its members inherit, so an admin assigns
// access to a team once. The row Drawer holds the members (add / remove) and the
// grant builder (which its members inherit). Gated by principal_group.

const columns: FlatColumn<Group>[] = [
  {
    key: "name", label: "Name", sortVal: (g) => groupName(g).toLowerCase(), cell: (g) => (
      <span>
        <span class="font-semibold">{groupName(g)}</span>
        <Show when={g.display_name && g.name !== g.display_name}>
          <span class="ml-1.5 font-data text-xs text-base-content/40">{g.name}</span>
        </Show>
      </span>
    ),
  },
  { key: "description", label: "Description", cell: (g) => <span class="text-sm text-base-content/60">{g.description || ""}</span> },
];

const filterKeys: FilterKey<Group>[] = [
  { key: "name", type: "string", hint: "substring", get: (g) => `${groupName(g)} ${g.name}` },
  { key: "description", type: "string", hint: "substring", get: (g) => g.description ?? "" },
];

export default function Groups() {
  const me = useMe();
  const groups = useQuery(() => ({ queryKey: GROUPS_KEY, queryFn: listGroups }));

  return (
    <FlatList<Group>
      config={{
        entity: { name: "principal_group", plural: "groups" },
        rows: () => groups.data ?? [],
        loading: () => groups.isPending,
        error: () => groups.error,
        filterKeys,
        filterPlaceholder: "filter by name or description",
        columns,
        empty: "No groups yet.",
        detail: (g) => ({ title: groupName(g), body: <GroupDetail id={g.id} /> }),
        create: can(me.data, "principal_group", "create")
          ? { label: "New group", can: () => can(me.data, "principal_group", "create"), body: (ctx) => <CreateGroupForm onCreated={(g) => ctx.select(g)} /> }
          : undefined,
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

// GroupDetail is the row's side-Drawer body: the group profile, its members (add
// and remove), the grant builder its members inherit, and delete. It re-derives
// the group from the live query by id so edits reflect immediately.
function GroupDetail(props: { id: string }) {
  const qc = useQueryClient();
  const me = useMe();
  const canManage = () => can(me.data, "principal_group", "update");
  const canDelete = () => can(me.data, "principal_group", "delete");
  const canGrant = () => can(me.data, "principal_grant", "create");
  const canRevoke = () => can(me.data, "principal_grant", "delete");

  const group = useQuery(() => ({ queryKey: [...GROUPS_KEY, props.id], queryFn: () => getGroup(props.id) }));
  const members = useQuery(() => ({ queryKey: [...GROUPS_KEY, props.id, "members"], queryFn: () => listGroupMembers(props.id) }));
  const principals = useQuery(() => ({ queryKey: PRINCIPALS_KEY, queryFn: () => listPrincipals(), enabled: canManage() }));

  const memberIDs = createMemo(() => new Set((members.data ?? []).map((m) => m.principal_id)));
  const addable = createMemo(() => (principals.data ?? []).filter((p) => !memberIDs().has(p.id)));

  const [toAdd, setToAdd] = createSignal("");
  const [err, setErr] = createSignal<string | null>(null);
  const refetchMembers = () => qc.invalidateQueries({ queryKey: [...GROUPS_KEY, props.id, "members"] });

  async function add() {
    const pid = toAdd();
    if (!pid) return;
    setErr(null);
    try {
      await addGroupMember(props.id, pid);
      setToAdd("");
      await refetchMembers();
    } catch (e) {
      setErr(describeError(e));
    }
  }
  async function remove(pid: string) {
    setErr(null);
    try {
      await removeGroupMember(props.id, pid);
      await refetchMembers();
    } catch (e) {
      setErr(describeError(e));
    }
  }
  async function removeGroup() {
    if (!confirm(`Delete the group "${group.data ? groupName(group.data) : ""}"? Members keep their direct grants; they stop inheriting this group's.`)) return;
    try {
      await deleteGroup(props.id);
      await qc.invalidateQueries({ queryKey: GROUPS_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  return (
    <div class="flex flex-col gap-5">
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>

      <div class="grid grid-cols-2 gap-3">
        <Fact label="Name" value={<span class="font-data">{group.data?.name}</span>} />
        <Fact label="Members" value={members.data?.length ?? 0} />
      </div>
      <Show when={group.data?.description}>
        <Fact label="Description" value={<span class="text-sm">{group.data!.description}</span>} />
      </Show>

      <div>
        <div class="eyebrow mb-1.5">Members</div>
        <div class="flex flex-col gap-1.5">
          <For each={members.data ?? []} fallback={<p class="text-sm text-base-content/40">No members yet.</p>}>
            {(m) => (
              <div class="flex items-center justify-between rounded-field border border-base-300 bg-base-100 px-2.5 py-1.5">
                <span class="min-w-0 truncate">
                  <span class="font-data text-sm">{memberName(m)}</span>
                  <span class="ml-1.5 badge badge-ghost badge-xs">{m.kind}</span>
                </span>
                <Show when={canManage()}>
                  <button class="btn btn-quiet btn-xs btn-square text-base-content/50" title="Remove from group" onClick={() => remove(m.principal_id)}>
                    <Trash size={14} />
                  </button>
                </Show>
              </div>
            )}
          </For>
        </div>
        <Show when={canManage()}>
          <div class="mt-2 flex items-center gap-2">
            <select class="select select-bordered select-sm min-w-0 flex-1 font-data" value={toAdd()} onChange={(e) => setToAdd(e.currentTarget.value)}>
              <option value="">Add a member...</option>
              <For each={addable()}>{(p) => <option value={p.id}>{principalName(p)}</option>}</For>
            </select>
            <button class="btn btn-action btn-sm gap-1.5" disabled={!toAdd()} onClick={add}><Plus size={14} /> Add</button>
          </div>
        </Show>
      </div>

      <div>
        <div class="eyebrow mb-1.5">Grants (inherited by every member)</div>
        <GroupGrantEditor id={props.id} canGrant={canGrant()} canRevoke={canRevoke()} />
      </div>

      <Show when={canDelete()}>
        <div class="border-t border-base-300 pt-4">
          <button class="btn btn-danger btn-sm gap-1.5" onClick={removeGroup}><Trash size={14} /> Delete group</button>
        </div>
      </Show>
    </div>
  );
}

// GroupGrantEditor wires the shared GrantBuilder to a group's grants: the same
// builder the user detail uses, with the group grant endpoints behind onSave. The
// escalation cover-check is on the server, exactly as for a direct grant.
function GroupGrantEditor(props: { id: string; canGrant: boolean; canRevoke: boolean }) {
  const qc = useQueryClient();
  const needTrees = () => props.canGrant || props.canRevoke;
  const grants = useQuery(() => ({ queryKey: [...GROUPS_KEY, props.id, "grants"], queryFn: () => listGroupGrants(props.id) }));
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
    (grants.data ?? []).filter((g) => g.id).map((g) => ({ id: g.id!, role: g.role, scope_kind: g.scope_kind as ScopeKind, scope_id: g.scope_id ?? undefined, scope_op: (g.scope_op as ScopeOp) || undefined })),
  );
  const entities = (kind: "location" | "system" | "component"): TreeNode[] => {
    const list = kind === "location" ? locations.data ?? [] : kind === "system" ? systems.data ?? [] : components.data ?? [];
    return list.map((e) => ({ id: e.id, value: e.id, label: e.name, parentId: e.parent_id, rank: 0 }));
  };

  async function onSave(diff: { adds: GrantRef[]; removes: ExistingGrant[] }) {
    try {
      for (const a of diff.adds) {
        await createGroupGrant(props.id, { role: a.role, scope_kind: a.scope_kind, scope_id: a.scope_kind === "all" ? undefined : a.scope_id, scope_op: a.scope_kind === "all" ? undefined : a.scope_op });
      }
      for (const r of diff.removes) {
        await revokeGroupGrant(props.id, r.id);
      }
    } finally {
      await qc.invalidateQueries({ queryKey: [...GROUPS_KEY, props.id, "grants"] });
    }
  }

  return (
    <GrantBuilder
      principalId={props.id}
      current={current()}
      roles={(roles.data ?? []).map((r) => ({ id: r.id, label: r.display_name || r.id, title: [r.description, `Grants: ${(r.effective_permissions ?? r.permissions).join(", ")}`].filter(Boolean).join("\n\n") }))}
      entities={entities}
      scopeName={(id) => nameOf().get(id)}
      canGrant={props.canGrant}
      canRevoke={props.canRevoke}
      onSave={onSave}
    />
  );
}

// CreateGroupForm is the new-group form the create Drawer hosts. On success it
// invalidates the list and hands the created group to onCreated, which opens its
// detail Drawer, so an admin lands on it to add members and grants.
function CreateGroupForm(props: { onCreated: (g: Group) => void }) {
  const qc = useQueryClient();
  const [name, setName] = createSignal("");
  const [displayName, setDisplayName] = createSignal("");
  const [description, setDescription] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      const g = await createGroup({ name: name().trim(), display_name: displayName().trim() || undefined, description: description().trim() || undefined });
      await qc.invalidateQueries({ queryKey: GROUPS_KEY });
      props.onCreated(g);
    } catch (e2) {
      setErr(describeError(e2));
      setBusy(false);
    }
  }

  return (
    <form class="flex flex-col gap-3" onSubmit={submit}>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <label class="flex flex-col gap-1">
        <span class="eyebrow">Name</span>
        <input class="input input-bordered w-full font-data" value={name()} placeholder="field-crew" onInput={(e) => setName(e.currentTarget.value)} disabled={busy()} required />
      </label>
      <label class="flex flex-col gap-1">
        <span class="eyebrow">Display name</span>
        <input class="input input-bordered w-full" value={displayName()} placeholder="Field Crew" onInput={(e) => setDisplayName(e.currentTarget.value)} disabled={busy()} />
      </label>
      <label class="flex flex-col gap-1">
        <span class="eyebrow">Description</span>
        <input class="input input-bordered w-full" value={description()} onInput={(e) => setDescription(e.currentTarget.value)} disabled={busy()} />
      </label>
      <div class="mt-1 flex justify-end">
        <button type="submit" class="btn btn-action btn-sm" disabled={busy() || !name().trim()}>{busy() ? "Creating..." : "Create group"}</button>
      </div>
    </form>
  );
}
