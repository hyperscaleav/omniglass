import { api } from "../api/client";
import type { components } from "../api/schema.gen";

// The notification types data layer (#618): thin typed wrappers over the
// /notification-types surface. A notification type says WHAT a message about an
// alarm says, as two templates (a one-line subject and a body text) rendered
// through the same sandbox a label rule runs in, over the alarm facts. Official
// types ship with Omniglass and are read-only.

export type NotificationTypeRow = components["schemas"]["NotificationTypeBody"];
export type NotificationPreview = components["schemas"]["PreviewNotificationTypeOutputBody"];

export const NOTIFICATION_TYPES_KEY = ["notification_types"] as const;

export async function listNotificationTypes(): Promise<NotificationTypeRow[]> {
  const { data, error } = await api.GET("/notification-types");
  if (error) throw error;
  return (data?.notification_types ?? []) as NotificationTypeRow[];
}

export type CreateNotificationType = {
  name: string;
  label?: string;
  description?: string;
  subject: string;
  text: string;
};

export async function createNotificationType(body: CreateNotificationType): Promise<NotificationTypeRow> {
  const { data, error } = await api.POST("/notification-types", { body });
  if (error) throw error;
  return data as NotificationTypeRow;
}

export type UpdateNotificationType = {
  label?: string;
  description?: string;
  subject?: string;
  text?: string;
};

export async function updateNotificationType(name: string, body: UpdateNotificationType): Promise<void> {
  const { error } = await api.PATCH("/notification-types/{name}", { params: { path: { name } }, body });
  if (error) throw error;
}

export async function deleteNotificationType(name: string): Promise<void> {
  const { error } = await api.DELETE("/notification-types/{name}", { params: { path: { name } } });
  if (error) throw error;
}

// A preview names a component and, optionally, the edge to render for; the
// alarm is the component's most recently raised one. Drafts of either template
// render instead of the stored ones, so an editor previews before it saves.
export type PreviewRequest = {
  component: string;
  transition?: "raised" | "cleared";
  subject?: string;
  text?: string;
};

export async function previewNotificationType(name: string, body: PreviewRequest): Promise<NotificationPreview> {
  const { data, error } = await api.POST("/notification-types/{name}:preview", { params: { path: { name } }, body });
  if (error) throw error;
  return data as NotificationPreview;
}

