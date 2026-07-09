import { createEffect, createMemo, createSignal, on, Show } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import GrantBuilder from "../components/GrantBuilder";
import { Fact, RelatedList } from "../components/DetailShell";
import { useBlades, useBladeEdit } from "../lib/blades";
import type { BladeDef } from "../lib/blades";
import type { TreeNode } from "../lib/treeselect";
import type { ExistingGrant, GrantRef, ScopeOp } from "../lib/grantdraft";
import {
  GROUPS_KEY, groupName, memberName,
  getGroup, updateGroup, deleteGroup,
  listGroupMembers, addGroupMember, removeGroupMember,
  listGroupGrants, createGroupGrant, revokeGroupGrant,
} from "../lib/groups";
import { type ScopeKind, PRINCIPALS_KEY, ROLES_KEY, listPrincipals, listRoles, principalName } from "../lib/principals";
import { listLocations } from "../lib/locations";
import { listSystems } from "../lib/systems";
import { listComponents } from "../lib/components";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";

// GroupDetail is a group's blade body under the read -> Edit -> Save contract. Read
// mode shows the profile, members (a drill to the member's user blade when the
// Groups page is the root), and inherited grants, all read-only. Edit mode (the
// header pencil) turns display name / description into inputs, stages member add /
// remove, activates the grant builder, and reveals a red Delete in the footer;
// Save commits the whole session, Cancel reverts. The group's own edits stay
// staged locally until Save so the operator can check their work first.
export function GroupDetail(props: { id: string }) {
  const qc = useQueryClient();
  const me = useMe();
  const blades = useBlades();
  const edit = useBladeEdit();
  const canManage = () => can(me.data, "principal_group", "update");
  const canDelete = () => can(me.data, "principal_group", "delete");
  const canGrant = () => can(me.data, "principal_grant", "create");
  const canRevoke = () => can(me.data, "principal_grant", "delete");

  const group = useQuery(() => ({ queryKey: [...GROUPS_KEY, props.id], queryFn: () => getGroup(props.id) }));
  const members = useQuery(() => ({ queryKey: [...GROUPS_KEY, props.id, "members"], queryFn: () => listGroupMembers(props.id) }));
  const principals = useQuery(() => ({ queryKey: PRINCIPALS_KEY, queryFn: () => listPrincipals(), enabled: canManage() }));

  const editing = () => edit.editing();
  const canDrillMembers = () => blades.stack()[0]?.kind === "group";

  // Staged member changes (applied on Save): principals to add, and current members
  // to remove. The list below always shows the effective post-save membership.
  const [pendAdd, setPendAdd] = createSignal<string[]>([]);
  const [pendRemove, setPendRemove] = createSignal<Set<string>>(new Set());
  const [displayName, setDisplayName] = createSignal("");
  const [description, setDescription] = createSignal("");
  const [err, setErr] = createSignal<string | null>(null);
  let grantCommit: () => Promise<void> = async () => {};
  let grantCancel: () => void = () => {};

  // Seed the profile inputs from the group when edit mode opens (untracked on the
  // group data, so a background refetch does not clobber in-progress edits).
  createEffect(on(() => edit.editing(), (isEditing) => {
    if (isEditing) {
      setDisplayName(group.data?.display_name ?? "");
      setDescription(group.data?.description ?? "");
    }
  }));

  const principalById = (id: string) => (principals.data ?? []).find((p) => p.id === id);
  const memberItems = createMemo(() => {
    const kept = (members.data ?? [])
      .filter((m) => !pendRemove().has(m.principal_id))
      .map((m) => ({ id: m.principal_id, kind: "user", name: memberName(m), badge: m.kind }));
    const added = pendAdd().map((id) => {
      const p = principalById(id);
      return { id, kind: "user", name: p ? principalName(p) : id, badge: p?.kind ?? "" };
    });
    return [...kept, ...added];
  });
  const shownIds = createMemo(() => new Set(memberItems().map((i) => i.id)));
  const addable = createMemo(() => (principals.data ?? []).filter((p) => !shownIds().has(p.id)));

  const stageRemove = (id: string) => {
    if (pendAdd().includes(id)) setPendAdd((a) => a.filter((x) => x !== id));
    else setPendRemove((s) => { const next = new Set(s); next.add(id); return next; });
  };
  const stageAdd = (id: string) => {
    if (id && !pendAdd().includes(id)) setPendAdd((a) => [...a, id]);
  };
  const resetStaging = () => {
    setPendAdd([]);
    setPendRemove(new Set<string>());
    setErr(null);
  };

  edit.bind({
    editable: canManage,
    destructive: () => (canDelete() ? { label: "Delete group", tone: "danger", onClick: removeGroup } : undefined),
    save: async () => {
      setErr(null);
      try {
        if (displayName() !== (group.data?.display_name ?? "") || description() !== (group.data?.description ?? "")) {
          await updateGroup(props.id, { display_name: displayName().trim() || undefined, description: description().trim() || undefined });
        }
        for (const id of pendRemove()) await removeGroupMember(props.id, id);
        for (const id of pendAdd()) await addGroupMember(props.id, id);
        await grantCommit();
        await Promise.all([
          qc.invalidateQueries({ queryKey: [...GROUPS_KEY, props.id] }),
          qc.invalidateQueries({ queryKey: [...GROUPS_KEY, props.id, "members"] }),
          qc.invalidateQueries({ queryKey: GROUPS_KEY }),
        ]);
        resetStaging();
      } catch (e) {
        setErr(describeError(e));
        throw e; // keep edit mode open so the operator can retry
      }
    },
    cancel: () => {
      resetStaging();
      grantCancel();
    },
  });

  async function removeGroup() {
    if (!confirm(`Delete the group "${group.data ? groupName(group.data) : ""}"? Members keep their direct grants; they stop inheriting this group's.`)) return;
    try {
      await deleteGroup(props.id);
      // The group is gone, so close the blade first (unmounting its detail, members,
      // and grants queries) and drop the dead detail caches, then refresh the list.
      // Refreshing before closing would refetch the deleted id and 404.
      blades.close();
      qc.removeQueries({ queryKey: [...GROUPS_KEY, props.id] });
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
        <Fact label="Members" value={<span class="tnum text-sm">{memberItems().length}</span>} />
      </div>

      <Show
        when={editing()}
        fallback={
          <Show when={group.data?.description}>
            <Fact label="Description" value={<span class="text-sm">{group.data!.description}</span>} />
          </Show>
        }
      >
        <div class="flex flex-col gap-3">
          <label class="flex flex-col gap-1">
            <span class="eyebrow">Display name</span>
            <input class="input input-bordered w-full" value={displayName()} placeholder="Field Crew" onInput={(e) => setDisplayName(e.currentTarget.value)} />
          </label>
          <label class="flex flex-col gap-1">
            <span class="eyebrow">Description</span>
            <input class="input input-bordered w-full" value={description()} onInput={(e) => setDescription(e.currentTarget.value)} />
          </label>
        </div>
      </Show>

      <RelatedList
        label="Members"
        items={memberItems()}
        empty="No members yet."
        onOpen={!editing() && canDrillMembers() ? (item) => blades.push({ kind: "user", id: item.id }) : undefined}
        onRemove={editing() ? (item) => stageRemove(item.id) : undefined}
        add={editing() ? { placeholder: "Add a member...", options: addable().map((p) => ({ id: p.id, label: principalName(p) })), onAdd: stageAdd, canAdd: true } : undefined}
      />

      <div>
        <div class="eyebrow mb-1.5">Grants (inherited by every member)</div>
        <GroupGrantEditor
          id={props.id}
          editing={editing()}
          canGrant={canGrant()}
          canRevoke={canRevoke()}
          onBind={(h) => { grantCommit = h.commit; grantCancel = h.cancel; }}
        />
      </div>
    </div>
  );
}

// GroupGrantEditor wires the shared GrantBuilder to a group's grants. In read mode
// the builder shows chips only (canGrant / canRevoke gated on `editing`); in edit
// mode it goes live and binds its commit up, so the blade's Save applies the grant
// diff as part of one edit session.
function GroupGrantEditor(props: { id: string; editing: boolean; canGrant: boolean; canRevoke: boolean; onBind: (h: { commit: () => Promise<void>; cancel: () => void; dirty: () => boolean }) => void }) {
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
    for (const a of diff.adds) {
      await createGroupGrant(props.id, { role: a.role, scope_kind: a.scope_kind, scope_id: a.scope_kind === "all" ? undefined : a.scope_id, scope_op: a.scope_kind === "all" ? undefined : a.scope_op });
    }
    for (const r of diff.removes) {
      await revokeGroupGrant(props.id, r.id);
    }
    await qc.invalidateQueries({ queryKey: [...GROUPS_KEY, props.id, "grants"] });
  }

  return (
    <GrantBuilder
      principalId={props.id}
      current={current()}
      roles={(roles.data ?? []).map((r) => ({ id: r.id, label: r.display_name || r.id, title: [r.description, `Grants: ${(r.effective_permissions ?? r.permissions).join(", ")}`].filter(Boolean).join("\n\n") }))}
      entities={entities}
      scopeName={(id) => nameOf().get(id)}
      canGrant={props.editing && props.canGrant}
      canRevoke={props.editing && props.canRevoke}
      onSave={onSave}
      bind={props.onBind}
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
