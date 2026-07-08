import { createMemo, createSignal, Show } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import GrantBuilder from "../components/GrantBuilder";
import { Fact, RelatedList, DetailActions } from "../components/DetailShell";
import { useBlades, type BladeDef } from "../lib/blades";
import type { TreeNode } from "../lib/treeselect";
import type { ExistingGrant, GrantRef, ScopeOp } from "../lib/grantdraft";
import {
  GROUPS_KEY, groupName, memberName,
  getGroup, deleteGroup,
  listGroupMembers, addGroupMember, removeGroupMember,
  listGroupGrants, createGroupGrant, revokeGroupGrant,
} from "../lib/groups";
import { type ScopeKind, PRINCIPALS_KEY, ROLES_KEY, listPrincipals, listRoles, principalName } from "../lib/principals";
import { listLocations } from "../lib/locations";
import { listSystems } from "../lib/systems";
import { listComponents } from "../lib/components";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";

// GroupDetail is a group's blade body (rooted on the Groups page, or a terminal
// blade opened from a user's group list / inherited grant). It re-derives the group
// by id so edits reflect immediately: the profile facts, its members (a drill to
// the member's user blade when the Groups page is the root, plus remove / add), the
// grant builder its members inherit, and delete.
export function GroupDetail(props: { id: string }) {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const canManage = () => can(me.data, "principal_group", "update");
  const canDelete = () => can(me.data, "principal_group", "delete");
  const canGrant = () => can(me.data, "principal_grant", "create");
  const canRevoke = () => can(me.data, "principal_grant", "delete");

  const group = useQuery(() => ({ queryKey: [...GROUPS_KEY, props.id], queryFn: () => getGroup(props.id) }));
  const members = useQuery(() => ({ queryKey: [...GROUPS_KEY, props.id, "members"], queryFn: () => listGroupMembers(props.id) }));
  const principals = useQuery(() => ({ queryKey: PRINCIPALS_KEY, queryFn: () => listPrincipals(), enabled: canManage() }));

  const memberIDs = createMemo(() => new Set((members.data ?? []).map((m) => m.principal_id)));
  const addable = createMemo(() => (principals.data ?? []).filter((p) => !memberIDs().has(p.id)));
  // Members drill to the user blade only when the Groups page is the stack root
  // (group -> user). Opened as a terminal blade from a user (root = user), the
  // members are a read-only reference.
  const canDrillMembers = () => blades.stack()[0]?.kind === "group";

  const [err, setErr] = createSignal<string | null>(null);
  const refetchMembers = () => qc.invalidateQueries({ queryKey: [...GROUPS_KEY, props.id, "members"] });

  async function add(pid: string) {
    if (!pid) return;
    setErr(null);
    try {
      await addGroupMember(props.id, pid);
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
      blades.close();
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
        <Fact label="Members" value={<span class="tnum text-sm">{members.data?.length ?? 0}</span>} />
      </div>
      <Show when={group.data?.description}>
        <Fact label="Description" value={<span class="text-sm">{group.data!.description}</span>} />
      </Show>

      <RelatedList
        label="Members"
        items={(members.data ?? []).map((m) => ({ id: m.principal_id, kind: "user", name: memberName(m), badge: m.kind }))}
        empty="No members yet."
        onOpen={canDrillMembers() ? (item) => blades.push({ kind: "user", id: item.id }) : undefined}
        onRemove={canManage() ? (item) => remove(item.id) : undefined}
        add={{ placeholder: "Add a member...", options: addable().map((p) => ({ id: p.id, label: principalName(p) })), onAdd: add, canAdd: canManage() }}
      />

      <div>
        <div class="eyebrow mb-1.5">Grants (inherited by every member)</div>
        <GroupGrantEditor id={props.id} canGrant={canGrant()} canRevoke={canRevoke()} />
      </div>

      <Show when={canDelete()}>
        <DetailActions destructive={<button class="btn btn-danger btn-sm" onClick={removeGroup}>Delete group</button>} />
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

function GroupTitle(props: { id: string }) {
  const group = useQuery(() => ({ queryKey: [...GROUPS_KEY, props.id], queryFn: () => getGroup(props.id) }));
  return <>{group.data ? groupName(group.data) : "Group"}</>;
}

export const groupBlade: BladeDef = {
  Title: (p) => <GroupTitle id={p.id} />,
  Body: (p) => <GroupDetail id={p.id} />,
};
