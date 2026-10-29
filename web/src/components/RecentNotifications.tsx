import { For, Show, type JSX } from "solid-js";
import { A } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Eyebrow from "./Eyebrow";
import { RECENT_SHOWN, listRecentNotifications, ofRule, recentNotificationsKey, type Notification } from "../lib/action_rules";
import { PILL_NEUTRAL, TRANSITION_PILL } from "../lib/subscriptions";
import { describeError, fmtTime, rel } from "../lib/format";

// RecentNotifications is a blade's view of what an action rule or a target has
// sent lately (#1346): each message's component, the edge it is about, where
// it went and how its delivery stands, newest first, read from GET
// /notifications (scoped by the caller's component read, exactly as the
// alarms it is about). A target's rows are narrowed by the server; a rule's
// are picked out of the newest RECENT_WINDOW, since the route has no rule
// filter. It polls, so a test alarm's delivery moves from queued to sent in
// place.

const POLL_MS = 10_000;

const STATUS: Record<Notification["status"], { word: string; cls: string }> = {
  queued: { word: "on its way", cls: "badge badge-sm badge-soft badge-info" },
  sent: { word: "sent", cls: "badge badge-sm badge-soft badge-success" },
  failed: { word: "failed", cls: "badge badge-sm badge-soft badge-error" },
  // A delivery a snooze held back (#1318): what the snooze was for, muted,
  // never a fault.
  suppressed: { word: "held by a snooze", cls: "badge badge-sm badge-ghost" },
};

export default function RecentNotifications(p: { rule?: string; target?: string; hint: string }): JSX.Element {
  const q = useQuery(() => ({
    queryKey: recentNotificationsKey({ target: p.target }),
    queryFn: () => listRecentNotifications({ target: p.target }),
    refetchInterval: POLL_MS,
  }));
  const rows = () => {
    const ns = q.data ?? [];
    return p.rule ? ofRule(ns, p.rule) : ns.slice(0, RECENT_SHOWN);
  };
  return (
    <section class="flex flex-col gap-2" aria-label="Recent notifications" data-testid="recent-notifications">
      <Eyebrow label="Recent notifications" hint={p.hint} />
      <Show when={!q.isPending} fallback={<span class="text-[12px] text-base-content/40">Loading…</span>}>
        <Show when={!q.error} fallback={<span role="alert" class="text-[12px] text-error">Could not read notifications: {describeError(q.error)}</span>}>
          <Show when={rows().length > 0} fallback={<span class="text-[12px] text-base-content/40">Nothing sent yet.</span>}>
            <ul class="flex flex-col divide-y divide-base-300 rounded-box border border-base-300">
              <For each={rows()}>
                {(n) => (
                  <li class="flex flex-col gap-1 px-3 py-2 text-sm" data-testid="recent-notification" data-status={n.status}>
                    <span class="flex min-w-0 items-center gap-2">
                      <A href={`/components/${n.component_id}`} class="truncate font-medium hover:underline">{n.component}</A>
                      <span class={TRANSITION_PILL[n.transition] ?? PILL_NEUTRAL}>{n.transition}</span>
                      <span class="ml-auto flex flex-none items-center gap-2">
                        <span class={STATUS[n.status]?.cls ?? PILL_NEUTRAL}>{STATUS[n.status]?.word ?? n.status}</span>
                        <span class="w-16 text-right text-[11px] tabular-nums text-base-content/50" title={fmtTime(n.created_at)}>{rel(n.created_at)}</span>
                      </span>
                    </span>
                    <span class="flex min-w-0 items-center gap-1.5 text-[11px] text-base-content/50">
                      <span class="truncate">
                        {p.rule ? "to " : "from "}
                        <span class="font-data text-base-content/70">{p.rule ? n.target : n.action_rule || "a subscription"}</span>
                        <Show when={n.recipient}>
                          {(who) => <> for <span class="font-data text-base-content/70">{who()}</span></>}
                        </Show>
                        {` by ${n.channel}`}
                      </span>
                    </span>
                    <Show when={n.last_error}>
                      <span
                        class="line-clamp-2 text-[11px] [overflow-wrap:anywhere]"
                        classList={{ "text-error": n.status === "failed", "text-base-content/50": n.status !== "failed" }}
                        title={n.last_error}
                        data-testid="recent-notification-error"
                      >
                        {n.status === "failed" ? n.last_error : `Retrying: ${n.last_error}`}
                      </span>
                    </Show>
                  </li>
                )}
              </For>
            </ul>
          </Show>
        </Show>
      </Show>
    </section>
  );
}
