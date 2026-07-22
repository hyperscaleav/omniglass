import { Show, createSignal } from "solid-js";
import { useSearchParams } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import type { FilterKey } from "../lib/predicate";
import { type Group, GROUPS_KEY, groupName, listGroups, createGroup, openGroupInEdit } from "../lib/groups";
import { identityRegistry } from "../lib/identityBlades";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { handleError } from "../lib/validate";
import { Plus } from "../components/icons";
import { useFormActions } from "../lib/formactions";

// Groups: the principal-group admin surface, a config over the shared FlatList. A
// group holds role x scope grants that its members inherit, so an admin assigns
// access to a team once. A row opens the group blade (rooted here on group, drilling
// into its members' user blades); the blade body lives in GroupDetail. Gated by
// principal_group.

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
  { key: "members", label: "Members", width: "110px", sortVal: (g) => g.member_count ?? 0, cell: (g) => <span class="tnum text-base-content/60">{g.member_count ?? 0}</span> },
  { key: "grants", label: "Grants", width: "100px", sortVal: (g) => g.grant_count ?? 0, cell: (g) => <span class="tnum text-base-content/60">{g.grant_count ?? 0}</span> },
  { key: "description", label: "Description", cell: (g) => <span class="text-sm text-base-content/60">{g.description || ""}</span> },
];

const filterKeys: FilterKey<Group>[] = [
  { key: "name", type: "string", hint: "substring", get: (g) => `${groupName(g)} ${g.name}` },
  { key: "description", type: "string", hint: "substring", get: (g) => g.description ?? "" },
];

export default function Groups() {
  const me = useMe();
  const [params] = useSearchParams();
  const groups = useQuery(() => ({ queryKey: GROUPS_KEY, queryFn: listGroups }));
  // ?g=<id> deep-links to a group (e.g. the cross-over from a user's inherited grant).
  const openId = () => (Array.isArray(params.g) ? params.g[0] : params.g) || undefined;

  return (
    <FlatList<Group>
      config={{
        entity: { name: "group", plural: "groups" },
        rows: () => groups.data ?? [],
        loading: () => groups.isPending,
        error: () => groups.error,
        filterKeys,
        filterPlaceholder: "filter by name or description",
        columns,
        empty: "No groups yet.",
        rowId: (g) => g.id,
        openId,
        blades: { registry: identityRegistry, rootKind: "group" },
        create: can(me.data, "principal_group", "create")
          ? { label: "New group", can: () => can(me.data, "principal_group", "create"), body: (ctx) => <CreateGroupForm onCreated={(g) => ctx.select(g)} onClose={ctx.close} /> }
          : undefined,
      }}
    />
  );
}

// CreateGroupForm is the new-group form the create Drawer hosts. On success it
// invalidates the list and hands the created group to onCreated, which opens its
// detail blade, so an admin lands on it to add members and grants.
function CreateGroupForm(props: { onCreated: (g: Group) => void; onClose: () => void }) {
  const qc = useQueryClient();
  const [name, setName] = createSignal("");
  const [displayName, setDisplayName] = createSignal("");
  const [description, setDescription] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  useFormActions().bind({
    submitLabel: "Create group",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => !name().trim() || !!handleError(name()),
    cancel: props.onClose,
  });

  async function submit() {
    setBusy(true);
    setErr(null);
    try {
      const g = await createGroup({ name: name().trim(), display_name: displayName().trim() || undefined, description: description().trim() || undefined });
      // Seed the new group's detail caches so its blade opens instantly (no loading
      // flash), and flag it to open in edit mode so members and grants can be added
      // right away, then hand it to the create Drawer's select to open it.
      qc.setQueryData([...GROUPS_KEY, g.id], g);
      qc.setQueryData([...GROUPS_KEY, g.id, "members"], []);
      qc.setQueryData([...GROUPS_KEY, g.id, "grants"], []);
      await qc.invalidateQueries({ queryKey: GROUPS_KEY });
      openGroupInEdit(g.id);
      props.onCreated(g);
    } catch (e2) {
      setErr(describeError(e2));
      setBusy(false);
    }
  }

  return (
    <form class="flex flex-col gap-3" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <p class="text-xs text-base-content/50">Creates a group. Members inherit the group's role grants; add members and grants afterwards.</p>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <label class="flex flex-col gap-1">
        <span class="eyebrow">Name</span>
        <input class="input input-bordered w-full font-data" classList={{ "input-error": !!handleError(name()) }} value={name()} placeholder="field-crew" onInput={(e) => setName(e.currentTarget.value)} disabled={busy()} required />
        <Show when={handleError(name())}>{(msg) => <p class="text-[11px] text-error">{msg()}</p>}</Show>
      </label>
      <label class="flex flex-col gap-1">
        <span class="eyebrow">Display name</span>
        <input class="input input-bordered w-full" value={displayName()} placeholder="Field Crew" onInput={(e) => setDisplayName(e.currentTarget.value)} disabled={busy()} />
      </label>
      <label class="flex flex-col gap-1">
        <span class="eyebrow">Description</span>
        <input class="input input-bordered w-full" value={description()} onInput={(e) => setDescription(e.currentTarget.value)} disabled={busy()} />
      </label>
    </form>
  );
}
