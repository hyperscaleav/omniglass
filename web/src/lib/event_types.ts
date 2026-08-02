import { api } from "../api/client";

// The Event Types catalog data layer: thin typed wrappers over the /event-types
// surface, the twin of the Properties catalog. An event type is a registered
// occurrence key (a dot-hierarchied name) that an occurrence is typed by. Official
// event types are seed-owned and read-only; custom ones are operator-created.
// payload_schema is an optional JSON Schema fragment for the occurrence payload.

export type EventTypeRow = {
  name: string;
  display_name?: string;
  description?: string;
  payload_schema?: unknown;
  official: boolean;
};

export const EVENT_TYPES_KEY = ["event_types"] as const;

export async function listEventTypes(): Promise<EventTypeRow[]> {
  const { data, error } = await api.GET("/event-types");
  if (error) throw error;
  return (data?.event_types ?? []) as EventTypeRow[];
}

export type CreateEventType = {
  name: string;
  display_name?: string;
  description?: string;
  payload_schema?: unknown;
};

export async function createEventType(body: CreateEventType): Promise<EventTypeRow> {
  const { data, error } = await api.POST("/event-types", { body });
  if (error) throw error;
  return data as EventTypeRow;
}

export type UpdateEventType = {
  display_name?: string;
  description?: string;
  payload_schema?: unknown;
};

export async function updateEventType(name: string, body: UpdateEventType): Promise<void> {
  const { error } = await api.PATCH("/event-types/{name}", { params: { path: { name } }, body });
  if (error) throw error;
}

export async function deleteEventType(name: string): Promise<void> {
  const { error } = await api.DELETE("/event-types/{name}", { params: { path: { name } } });
  if (error) throw error;
}
