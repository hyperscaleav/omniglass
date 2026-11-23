import { Show, type JSX } from "solid-js";
import AcknowledgeButton from "./AcknowledgeButton";
import { SnoozeControl } from "./SnoozeButton";
import TroubleshootButton from "./TroubleshootButton";
import Button from "./Button";
import { Check } from "./icons";
import type { createTimelineToggles } from "./AlarmTimeline";
import { can, useMe } from "../lib/auth";

// AlarmActions (#2115): the one group of controls an alarm offers wherever it
// is read, so Acknowledge, Snooze, Troubleshoot, Clear and the timeline toggle
// can never sit at different heights or split across lines again.
//
// One group: `inline-flex flex-nowrap items-center`, every member the same `xs`
// height on one center line. The group wraps as a unit under its alarm's message
// on a narrow container (`shrink-0` on the group, so its parent's flex-wrap
// moves it whole), never member by member. It centers itself (`self-center`)
// against a message row that aligns text on its baseline, because an icon-only
// square has no baseline and would otherwise fall to its bottom edge.
//
// Each member is gated on its own permission (a control the caller may not use
// is not drawn, and the group is not drawn when none is). Callers that already
// know the permissions (the alarms panel) pass `allow`. Only this file imports
// AcknowledgeButton and SnoozeControl (alarm-actions-guard.test.ts).
export type AlarmActionsAlarm = {
  id: string;
  message?: string;
  acknowledged?: boolean;
  snoozed_until?: string;
  snoozed_by?: string;
  // Troubleshoot hands omnibot the alarm by its component's name and rule.
  component?: string;
  raised_by_rule?: string | null;
  raised_at?: string;
};

export type AlarmActionsShow = {
  ack?: boolean;
  snooze?: boolean;
  troubleshoot?: boolean;
  // The open state of the caller's timelines, from createTimelineToggles.
  timeline?: ReturnType<typeof createTimelineToggles>;
  // Clear, for a surface that offers it: the caller owns the write.
  clear?: { allowed: boolean; busy?: boolean; onClear: () => void };
};

export default function AlarmActions(props: {
  // The component's uuid, never its name: a name repeats across the fleet and
  // addresses nothing where it does (ADR-0062, #2441).
  componentId: string;
  alarm: AlarmActionsAlarm;
  // The snooze menu's surface key (a createUniqueId the list owns).
  surface?: string;
  show: AlarmActionsShow;
  allow?: { ack?: boolean; snooze?: boolean };
}): JSX.Element {
  const me = useMe();
  const snoozeAllowed = () => props.allow?.snooze ?? can(me.data, "alarm", "snooze");
  const troubleshootable = () =>
    props.show.troubleshoot && props.alarm.component && props.alarm.raised_at
      ? { component: props.alarm.component, raised_by_rule: props.alarm.raised_by_rule, raised_at: props.alarm.raised_at, message: props.alarm.message }
      : undefined;
  // Members that will draw, by the same gates their own components apply: the
  // group is not drawn at all when none will, so an empty flex item never takes
  // a gap in its row.
  const hasMember = () =>
    !!(
      (props.show.ack && !props.alarm.acknowledged && (props.allow?.ack ?? can(me.data, "alarm", "acknowledge"))) ||
      (props.show.clear && props.show.clear.allowed) ||
      (props.show.snooze && snoozeAllowed()) ||
      (troubleshootable() && can(me.data, "assistant", "use")) ||
      props.show.timeline
    );
  return (
    <Show when={hasMember()}>
    <div data-alarm-actions class="relative inline-flex shrink-0 flex-nowrap items-center gap-1.5 self-center">
      <Show when={props.show.ack}>
        <AcknowledgeButton componentId={props.componentId} alarm={props.alarm} allowed={props.allow?.ack} />
      </Show>
      <Show when={props.show.clear}>
        {(c) => (
          <Show when={c().allowed}>
            <Button square size="xs" icon={Check} label={`Clear alarm ${props.alarm.id}`} title="Clear this alarm" disabled={c().busy} onClick={() => c().onClear()} />
          </Show>
        )}
      </Show>
      <Show when={props.show.snooze}>
        <SnoozeControl componentId={props.componentId} alarm={props.alarm} surface={props.surface ?? "alarm-actions"} allowed={snoozeAllowed()} />
      </Show>
      <Show when={troubleshootable()}>{(t) => <TroubleshootButton alarm={t()} />}</Show>
      <Show when={props.show.timeline}>{(tl) => tl().button(props.alarm.id, props.alarm.message ?? "")}</Show>
    </div>
    </Show>
  );
}
