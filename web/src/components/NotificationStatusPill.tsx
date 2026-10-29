import type { JSX } from "solid-js";
import type { Notification } from "../lib/action_rules";

// NotificationStatusPill is the console's one word for how a notification's
// delivery stands (#1413), worn alike by an alarm's timeline and a blade's
// Recent notifications, so the two never word a status apart. A notification
// a snooze suppressed (#1318) was deliberately not sent: it reads "snoozed" in
// the neutral pill (PILL_NEUTRAL's tone), never as a failure. Each word's hint
// teaches what the status means for the message.
const STATUS: Record<Notification["status"], { word: string; tone: string; tip: string }> = {
  queued: { word: "on its way", tone: "badge-soft badge-info", tip: "Queued: the worker delivers it shortly, retrying with backoff if the target refuses" },
  sent: { word: "sent", tone: "badge-soft badge-success", tip: "Sent: the target accepted the message" },
  failed: { word: "failed", tone: "badge-soft badge-error", tip: "Failed: every attempt was refused, and it will not be tried again unless retried" },
  suppressed: { word: "snoozed", tone: "badge-ghost", tip: "Snoozed: the alarm's condition was snoozed, so the message was recorded and deliberately not sent" },
};

export default function NotificationStatusPill(props: { status: string; size?: "xs" | "sm" }): JSX.Element {
  const look = () => STATUS[props.status as Notification["status"]];
  return (
    <span
      class={`badge badge-${props.size ?? "sm"} ${look()?.tone ?? "badge-ghost"}`}
      data-testid="notification-status"
      data-status={props.status}
      title={look()?.tip}
    >
      {look()?.word ?? props.status}
    </span>
  );
}
