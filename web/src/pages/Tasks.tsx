import { For, Show, createMemo, createSignal } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import { Clock } from "../components/icons";
import {
  type Task,
  TASKS_KEY,
  TASK_MODES,
  taskLabel,
  listTasks,
  createTask,
  updateTask,
  deleteTask,
  taskFilterKeys,
} from "../lib/tasks";
import { INTERFACES_KEY, listInterfaces } from "../lib/interfaces";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";

// Tasks: the collection-work inventory, a config over the shared FlatList. A row per
// task with its interface, mode (poll/listen), enabled toggle, and node placement; a
// row opens the side Drawer detail (facts + an inline edit + delete); "New task"
// opens the create Drawer. A task's identity is content-addressed over its interface
// + mode + spec (so identical work dedupes), so name/mode are fixed and only the
// display name, enabled toggle, node, and spec are editable. Every gate is a UI hint;
// the server is the authority. Renders only real TaskBody fields.
function EnabledPill(props: { on: boolean }) {
  return <span class={`badge badge-soft badge-sm ${props.on ? "badge-success" : "badge-neutral"}`}>{props.on ? "enabled" : "disabled"}</span>;
}

const columns: FlatColumn<Task>[] = [
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
    key: "interface", label: "Interface", width: "180px", sortVal: (t) => t.interface.toLowerCase(),
    cell: (t) => <span class="font-data text-sm text-base-content/70">{t.interface}</span>,
  },
  {
    key: "mode", label: "Mode", width: "110px", sortVal: (t) => t.mode,
    cell: (t) => <span class="badge badge-soft badge-neutral badge-sm">{t.mode}</span>,
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

export default function Tasks() {
  const me = useMe();
  const tasks = useQuery(() => ({ queryKey: TASKS_KEY, queryFn: () => listTasks() }));

  return (
    <FlatList<Task>
      config={{
        entity: { name: "task", plural: "tasks" },
        rows: () => tasks.data ?? [],
        loading: () => tasks.isLoading,
        error: () => tasks.error,
        filterKeys: taskFilterKeys,
        filterPlaceholder: "filter by interface, mode, enabled",
        columns,
        empty: "No tasks yet.",
        detail: (t) => ({ title: <span class="font-data">{taskLabel(t)}</span>, body: <TaskDetail id={t.id} /> }),
        create: {
          label: "New task",
          can: () => can(me.data, "task", "create"),
          body: (ctx) => <CreateTaskForm close={ctx.close} onCreated={ctx.select} />,
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

// TaskDetail is the row's side-Drawer body. It re-derives the task from the live
// query by id (not the row snapshot), so an edit (e.g. an enabled toggle) reflects
// after the invalidate. The read view carries the facts, an inline edit of the
// mutable fields, and a delete; both actions are gated by the matching permission.
function TaskDetail(props: { id: string }) {
  const qc = useQueryClient();
  const me = useMe();
  const tasks = useQuery(() => ({ queryKey: TASKS_KEY, queryFn: () => listTasks() }));
  const t = createMemo(() => tasks.data?.find((x) => x.id === props.id) ?? null);
  const [editing, setEditing] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);

  async function del(task: Task) {
    if (!confirm(`Delete task "${taskLabel(task)}"?`)) return;
    setErr(null);
    setBusy(true);
    try {
      await deleteTask(task.id);
      await qc.invalidateQueries({ queryKey: TASKS_KEY });
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Show when={t()} fallback={<p class="py-8 text-center text-sm text-base-content/40">This task is no longer available.</p>}>
      {(task) => (
        <div class="flex flex-col gap-3">
          <div class="flex items-center gap-3">
            <span class="text-base-content/40"><Clock size={22} /></span>
            <span class="badge badge-soft badge-neutral badge-sm">{task().mode}</span>
            <EnabledPill on={task().enabled} />
          </div>

          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>

          <Show
            when={editing()}
            fallback={
              <>
                <div class="grid grid-cols-2 gap-3 text-sm">
                  <Fact label="Interface" value={<span class="font-data">{task().interface}</span>} />
                  <Fact label="Mode" value={task().mode} />
                  <Fact label="Node" value={task().node ? <span class="font-data">{task().node}</span> : <span class="text-base-content/40">unassigned</span>} />
                  <Fact label="ID" value={<span class="font-data text-xs text-base-content/50">{task().id}</span>} />
                </div>
                <div class="flex items-center gap-2 border-t border-base-300 pt-3">
                  <Show when={can(me.data, "task", "delete")}>
                    <button class="btn btn-danger btn-sm" onClick={() => del(task())} disabled={busy()}>Delete</button>
                  </Show>
                  <span class="flex-1" />
                  <Show when={can(me.data, "task", "update")}>
                    <button class="btn btn-action btn-sm" onClick={() => setEditing(true)}>Edit</button>
                  </Show>
                </div>
              </>
            }
          >
            <EditTaskForm task={task()} onDone={() => setEditing(false)} />
          </Show>
        </div>
      )}
    </Show>
  );
}

// EditTaskForm edits the mutable fields inline (no nested dialog): the display name
// and the enabled toggle (the worklist on/off). The interface and mode define the
// content-addressed identity and are fixed after creation.
function EditTaskForm(props: { task: Task; onDone: () => void }) {
  const qc = useQueryClient();
  const [displayName, setDisplayName] = createSignal(props.task.display_name ?? "");
  const [enabled, setEnabled] = createSignal(props.task.enabled);
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      const patch: { display_name?: string; enabled?: boolean } = {};
      if (displayName().trim() !== (props.task.display_name ?? "")) patch.display_name = displayName().trim();
      if (enabled() !== props.task.enabled) patch.enabled = enabled();
      await updateTask(props.task.id, patch);
      await qc.invalidateQueries({ queryKey: TASKS_KEY });
      props.onDone();
    } catch (er) {
      setErr(describeError(er));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form class="flex flex-col gap-3" onSubmit={submit}>
      <p class="text-xs text-base-content/50">Change the display name or toggle the task on/off the worklist. The interface and mode are fixed after creation.</p>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <div>
        <label class="eyebrow mb-1.5 block" for="edit-task-display">Display name</label>
        <input id="edit-task-display" autocomplete="off" class="input input-bordered w-full" value={displayName()} placeholder="HQ display ping" onInput={(e) => setDisplayName(e.currentTarget.value)} disabled={busy()} />
      </div>
      <label class="flex items-center gap-2.5 text-sm">
        <input type="checkbox" class="toggle toggle-sm" checked={enabled()} onChange={(e) => setEnabled(e.currentTarget.checked)} disabled={busy()} />
        On the worklist (enabled)
      </label>
      <div class="mt-1 flex justify-end gap-2">
        <button type="button" class="btn btn-quiet btn-sm" onClick={props.onDone} disabled={busy()}>Cancel</button>
        <button type="submit" class="btn btn-action btn-sm" disabled={busy()}>
          <Show when={busy()}><span class="loading loading-spinner loading-xs" /></Show>
          Save changes
        </button>
      </div>
    </form>
  );
}

// CreateTaskForm is the new-task form the create Drawer hosts: the interface it runs
// over (from the interfaces list), the mode (poll/listen), an optional display name,
// and the enabled toggle. On success it invalidates the list and hands the created
// task to onCreated, which opens its detail Drawer.
function CreateTaskForm(props: { close: () => void; onCreated: (t: Task) => void }) {
  const qc = useQueryClient();
  const interfaces = useQuery(() => ({ queryKey: INTERFACES_KEY, queryFn: () => listInterfaces() }));
  const [iface, setIface] = createSignal("");
  const [mode, setMode] = createSignal<string>(TASK_MODES[0]);
  const [displayName, setDisplayName] = createSignal("");
  const [enabled, setEnabled] = createSignal(true);
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      const created = await createTask({
        interface: iface(),
        mode: mode(),
        enabled: enabled(),
        display_name: displayName().trim() || undefined,
      });
      await qc.invalidateQueries({ queryKey: TASKS_KEY });
      props.onCreated(created);
    } catch (er) {
      setErr(describeError(er));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form class="flex flex-col gap-3" onSubmit={submit}>
      <p class="text-xs text-base-content/50">Schedules collection work over an interface. Poll runs it on a cadence; listen waits for the device to push.</p>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-task-interface">Interface</label>
        <select id="new-task-interface" class="select select-bordered w-full" value={iface()} onChange={(e) => setIface(e.currentTarget.value)} disabled={busy()} required>
          <option value="" disabled>Select an interface</option>
          <For each={interfaces.data}>{(i) => <option value={i.name}>{i.name} ({i.type})</option>}</For>
        </select>
      </div>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-task-mode">Mode</label>
        <select id="new-task-mode" class="select select-bordered w-full" value={mode()} onChange={(e) => setMode(e.currentTarget.value)} disabled={busy()}>
          <For each={TASK_MODES}>{(m) => <option value={m}>{m}</option>}</For>
        </select>
      </div>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-task-display">Display name</label>
        <input id="new-task-display" autocomplete="off" class="input input-bordered w-full" value={displayName()} placeholder="Optional" onInput={(e) => setDisplayName(e.currentTarget.value)} disabled={busy()} />
      </div>
      <label class="flex items-center gap-2.5 text-sm">
        <input type="checkbox" class="toggle toggle-sm" checked={enabled()} onChange={(e) => setEnabled(e.currentTarget.checked)} disabled={busy()} />
        On the worklist (enabled)
      </label>
      <div class="mt-1 flex justify-end gap-2">
        <button type="button" class="btn btn-quiet btn-sm" onClick={props.close} disabled={busy()}>Cancel</button>
        <button type="submit" class="btn btn-action btn-sm" disabled={busy() || !iface()}>
          <Show when={busy()}><span class="loading loading-spinner loading-xs" /></Show>
          Create task
        </button>
      </div>
    </form>
  );
}
