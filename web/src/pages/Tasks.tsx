import { Show, createMemo } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import { Clock } from "../components/icons";
import { type Task, TASKS_KEY, taskLabel, listTasks, taskFilterKeys } from "../lib/tasks";
import { INTERFACES_KEY, listInterfaces, type Interface } from "../lib/interfaces";

// nameOf resolves a task's interface_id to its friendly interface name from the
// loaded interfaces list; a task carries the surrogate id, not the name. Falls back
// to the id when the interface is not (yet) loaded, so a row is never blank.
function nameFor(interfaces: Interface[] | undefined, id: string): string {
  return interfaces?.find((i) => i.id === id)?.name ?? id;
}

// Tasks: the collection-work inventory, read-only over the shared FlatList. A task is
// DERIVED (created automatically when its interface is created, one poll task per
// interface) and is never operator-authored: there is no create/edit/delete. A row
// shows its interface, mode, enabled state, and node placement (projected from the
// interface); the detail Drawer is facts only. Renders only real TaskBody fields.
function EnabledPill(props: { on: boolean }) {
  // enabled is a soft green; disabled is a soft grey fill (a tint of the text color,
  // visible in both themes).
  return <span class={`badge badge-sm ${props.on ? "badge-soft badge-success" : "bg-base-content/10 text-base-content/70 border-transparent"}`}>{props.on ? "enabled" : "disabled"}</span>;
}

function taskColumns(nameOf: (id: string) => string): FlatColumn<Task>[] {
  return [
    {
      key: "name", label: "Task", sortVal: (t) => taskLabel(t).toLowerCase(),
      cell: (t) => (
        <div class="flex items-center gap-2.5">
          <span class="text-base-content/40"><Clock size={16} /></span>
          <div class="min-w-0 leading-tight">
            <div class="truncate text-sm font-medium">{taskLabel(t)}</div>
            <Show when={t.display_name}><div class="truncate font-data text-[11px] text-base-content/40">{t.id}</div></Show>
          </div>
        </div>
      ),
    },
    {
      key: "interface", label: "Interface", width: "180px", sortVal: (t) => nameOf(t.interface_id).toLowerCase(),
      cell: (t) => <span class="font-data text-sm text-base-content/70">{nameOf(t.interface_id)}</span>,
    },
    {
      key: "mode", label: "Mode", width: "110px", sortVal: (t) => t.mode,
      cell: (t) => <span class="badge badge-ghost badge-sm">{t.mode}</span>,
    },
    {
      key: "enabled", label: "Enabled", width: "120px", sortVal: (t) => String(t.enabled),
      cell: (t) => <EnabledPill on={t.enabled} />,
    },
    {
      key: "node", label: "Node", width: "150px", sortVal: (t) => (t.node ?? "").toLowerCase(),
      cell: (t) => (t.node ? <span class="font-data text-sm text-base-content/70">{t.node}</span> : <span class="text-base-content/30">-</span>),
    },
  ];
}

export default function Tasks() {
  const tasks = useQuery(() => ({ queryKey: TASKS_KEY, queryFn: () => listTasks() }));
  const interfaces = useQuery(() => ({ queryKey: INTERFACES_KEY, queryFn: () => listInterfaces() }));
  const nameOf = (id: string) => nameFor(interfaces.data, id);

  return (
    <FlatList<Task>
      config={{
        entity: { name: "task", plural: "tasks" },
        rows: () => tasks.data ?? [],
        loading: () => tasks.isLoading,
        error: () => tasks.error,
        filterKeys: taskFilterKeys(nameOf),
        filterPlaceholder: "filter by interface, mode, enabled",
        columns: taskColumns(nameOf),
        empty: "No tasks yet. A task is derived when you add an interface to a component.",
        detail: (t) => ({ title: <span class="font-data">{taskLabel(t)}</span>, body: <TaskDetail id={t.id} /> }),
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

// TaskDetail is the row's side-Drawer body, read-only: a task is derived plumbing,
// so the detail carries facts only (no edit, no delete). It re-derives the task from
// the live query by id, so it reflects a change after an invalidate.
function TaskDetail(props: { id: string }) {
  const tasks = useQuery(() => ({ queryKey: TASKS_KEY, queryFn: () => listTasks() }));
  const interfaces = useQuery(() => ({ queryKey: INTERFACES_KEY, queryFn: () => listInterfaces() }));
  const t = createMemo(() => tasks.data?.find((x) => x.id === props.id) ?? null);

  return (
    <Show when={t()} fallback={<p class="py-8 text-center text-sm text-base-content/40">This task is no longer available.</p>}>
      {(task) => (
        <div class="flex flex-col gap-3">
          <div class="flex items-center gap-3">
            <span class="text-base-content/40"><Clock size={22} /></span>
            <span class="badge badge-ghost badge-sm">{task().mode}</span>
            <EnabledPill on={task().enabled} />
          </div>
          <p class="text-xs text-base-content/50">Derived from its interface. Its node placement projects from the interface; it is not edited here.</p>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <Fact label="Interface" value={<span class="font-data">{nameFor(interfaces.data, task().interface_id)}</span>} />
            <Fact label="Mode" value={task().mode} />
            <Fact label="Node" value={task().node ? <span class="font-data">{task().node}</span> : <span class="text-base-content/40">unassigned</span>} />
            <Fact label="ID" value={<span class="font-data text-xs text-base-content/50">{task().id}</span>} />
          </div>
        </div>
      )}
    </Show>
  );
}
