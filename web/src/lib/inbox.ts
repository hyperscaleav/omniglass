import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { api } from "../api/client";
import type { components } from "../api/schema.gen";
import { can, useMe } from "./auth";

// The console inbox data layer (#1355, epic #1341): the caller's own
// person-addressed notifications on the inbox channel, newest first, with the
// unread count. Self-scoped by the server, and scoped again by the caller's
// component read scope as it stands at read time, so an entry about a component
// they can no longer read is simply gone. Gated by notification:read.

export type InboxEntry = components["schemas"]["NotificationBody"];
export type Inbox = { notifications: InboxEntry[]; unread: number };

// Every inbox read shares one prefix, so a mark (one entry or all) refreshes
// the shell's count and the page's list in a single invalidation.
export const INBOX_PREFIX = ["auth", "me", "notifications"] as const;
export const INBOX_COUNT_KEY = [...INBOX_PREFIX, "count"] as const;
export const inboxListKey = (unreadOnly: boolean) => [...INBOX_PREFIX, "list", unreadOnly ? "unread" : "all"] as const;

// How often the shell asks for the unread count. There is no live channel for
// alarms or notifications in the console today (every alarm surface polls), so
// the count polls too, at a pace that suits a badge rather than a live panel.
export const INBOX_POLL_MS = 30_000;

// How many entries the page lists.
export const INBOX_PAGE = 100;

export async function listInbox(opts: { unread?: boolean; limit?: number } = {}): Promise<Inbox> {
  const { data, error } = await api.GET("/auth/me/notifications", {
    params: { query: { unread: opts.unread || undefined, limit: opts.limit } },
  });
  if (error) throw error;
  return { notifications: (data?.notifications ?? []) as InboxEntry[], unread: data?.unread ?? 0 };
}

// unreadCount reads the count alone: one entry is the least the route returns,
// and the count it carries covers the whole inbox.
export async function unreadCount(): Promise<number> {
  return (await listInbox({ unread: true, limit: 1 })).unread;
}

export async function markRead(id: string): Promise<InboxEntry> {
  const { data, error } = await api.POST("/auth/me/notifications/{id}:markRead", { params: { path: { id } } });
  if (error) throw error;
  return data as InboxEntry;
}

export async function markAllRead(): Promise<number> {
  const { data, error } = await api.POST("/auth/me/notifications:markAllRead", {});
  if (error) throw error;
  return data?.marked ?? 0;
}

// useUnreadCount is the shell's badge query: polled, and never asked by a
// principal the server would refuse.
export function useUnreadCount() {
  const me = useMe();
  return useQuery(() => ({
    queryKey: INBOX_COUNT_KEY,
    queryFn: unreadCount,
    enabled: can(me.data, "notification", "read"),
    refetchInterval: INBOX_POLL_MS,
    retry: false,
  }));
}

// useInboxMarks returns the two marks, each refreshing every inbox read after
// it lands (the badge, both list filters).
export function useInboxMarks() {
  const qc = useQueryClient();
  const refresh = () => qc.invalidateQueries({ queryKey: INBOX_PREFIX });
  return {
    one: async (id: string) => {
      await markRead(id);
      await refresh();
    },
    all: async () => {
      const n = await markAllRead();
      await refresh();
      return n;
    },
  };
}

// badgeText is what the shell's count reads: nothing at zero, the number up to
// 99, and 99+ beyond, so the badge never grows wider than the bell it sits on.
export function badgeText(unread: number | undefined): string {
  if (!unread || unread <= 0) return "";
  return unread > 99 ? "99+" : String(unread);
}

// inboxEntryHref is where an entry leads: its component's Activity tab, where
// the alarm it is about sits with its timeline.
export function inboxEntryHref(e: Pick<InboxEntry, "component_id">): string {
  return `/components/${e.component_id}?tab=activity`;
}
