import { api } from "../api/client";

// The tasks data layer: thin typed READ wrappers over the generated client. A task
// is DERIVED (created when an interface is created) and read-only, so there is no
// create/update/delete client; its node placement projects from its interface. The
// node detail's Tasks panel reads these. Shapes follow the OpenAPI (see api/tasks.go,
// TaskBody).
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
