import { api } from "../api/client";
import type { FilterKey } from "./predicate";

// The tasks data layer: thin typed wrappers over the generated client, so the Tasks
// page stays declarative and unit-testable against a mocked fetch. Shapes follow the
// OpenAPI (see api/tasks.go, TaskBody). A task is one unit of collection work over an
// interface: a mode (the poll/listen axis), an enabled toggle (whether it is on the
// worklist), an optional node placement, and inline probe settings in spec (jsonb).
// Its id is content-addressed server-side, so identical work dedupes.
export type Task = {
  id: string;
  display_name?: string;
  interface_id: string;
  mode: string;
  enabled: boolean;
  node?: string;
  spec?: unknown;
};

export const TASKS_KEY = ["tasks"] as const;

// The poll/listen axis a task runs on. The mode is a fixed enum server-side, so the
// create body's mode is the literal union, not a free string.
export const TASK_MODES = ["poll", "listen"] as const;
export type TaskMode = (typeof TASK_MODES)[number];

// taskLabel is the row's display: its display name when set, else its
// content-addressed id (the address).
export function taskLabel(t: Task): string {
  return t.display_name || t.id;
}

export async function listTasks(): Promise<Task[]> {
  const { data, error } = await api.GET("/tasks");
  if (error) throw error;
  return (data?.tasks ?? []) as Task[];
}

export async function getTask(id: string): Promise<Task> {
  const { data, error } = await api.GET("/tasks/{id}", { params: { path: { id } } });
  if (error) throw error;
  return data as Task;
}

export type CreateTask = {
  interface_id: string;
  mode: TaskMode;
  enabled?: boolean;
  display_name?: string;
  node?: string;
  spec?: unknown;
};

export async function createTask(body: CreateTask): Promise<Task> {
  const { data, error } = await api.POST("/tasks", { body });
  if (error) throw error;
  return data as Task;
}

// Only the display name, enabled toggle, node placement, and spec are mutable after
// creation (interface and mode define the content-addressed identity).
export type UpdateTask = { display_name?: string; enabled?: boolean; node?: string; spec?: unknown };

export async function updateTask(id: string, body: UpdateTask): Promise<Task> {
  const { data, error } = await api.PATCH("/tasks/{id}", { params: { path: { id } }, body });
  if (error) throw error;
  return data as Task;
}

export async function deleteTask(id: string): Promise<void> {
  const { error } = await api.DELETE("/tasks/{id}", { params: { path: { id } } });
  if (error) throw error;
}

// The faceted-search fields the shared FilterBar/ListShell drives: interface (exact),
// mode (exact, over poll/listen), and enabled (exact, over the boolean rendered as
// true/false). The interface facet resolves the task's interface_id to its friendly
// name via nameOf (a task carries the surrogate id, not the name), so both the chip
// value catalog and the match run over readable names. Matching is client-side over
// the loaded rows via lib/predicate.
export function taskFilterKeys(nameOf: (id: string) => string): FilterKey<Task>[] {
  return [
    { key: "interface", type: "string", hint: "exact", get: (t) => nameOf(t.interface_id), values: (rows) => [...new Set(rows.map((r) => nameOf(r.interface_id)).filter(Boolean))].sort() },
    { key: "mode", type: "string", hint: "exact", get: (t) => t.mode, values: (rows) => [...new Set(rows.map((r) => r.mode))].sort() },
    { key: "enabled", type: "string", hint: "exact", get: (t) => String(t.enabled), values: () => ["false", "true"] },
  ];
}
