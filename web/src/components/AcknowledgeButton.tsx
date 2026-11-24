import { Show, createSignal } from "solid-js";
import { useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import ActionFailure from "./ActionFailure";
import { Eye } from "./icons";
import { ALARM_TIMELINE_PREFIX, FLEET_ALARMS_PREFIX, acknowledgeAlarm, componentAlarmsKey } from "../lib/alarms";
import { FLEET_VIEW_KEY } from "../lib/fleet";
import { SYSTEM_HEALTH_PREFIX } from "../lib/health";
import { can, useMe } from "../lib/auth";
import { describeError } from "../lib/format";

// AcknowledgeButton (#872): "I have seen this", wherever an alarm is read as
// the reason. Acknowledging is gated on its own permission, never on the
// component's update (ADR-0109): it records that the reader looked, writes
// none of the component's data, and recomputes no health. It renders nothing
// for an alarm already acknowledged or a caller who may not.
export default function AcknowledgeButton(props: { componentId: string; alarm: { id: string; message?: string; acknowledged?: boolean }; allowed?: boolean }) {
  const me = useMe();
  const qc = useQueryClient();
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);
  const ack = async () => {
    setBusy(true);
    setErr(null);
    try {
      await acknowledgeAlarm(props.componentId, props.alarm.id);
      await Promise.all([
        qc.invalidateQueries({ queryKey: [...componentAlarmsKey(props.componentId)] }),
        qc.invalidateQueries({ queryKey: [...FLEET_VIEW_KEY] }),
        // The alarm queue (#2333) reads the same fact fleet-wide.
        qc.invalidateQueries({ queryKey: [...FLEET_ALARMS_PREFIX] }),
        qc.invalidateQueries({ queryKey: [...ALARM_TIMELINE_PREFIX] }),
        // The system page's Why strip reads the acknowledgement from the health
        // report (#990); keyed by system, so the prefix refreshes whichever is open.
        qc.invalidateQueries({ queryKey: [...SYSTEM_HEALTH_PREFIX] }),
      ]);
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Show when={!props.alarm.acknowledged && (props.allowed ?? can(me.data, "alarm", "acknowledge"))}>
      <Button size="xs" intent="quiet" icon={Eye} loading={busy()} title="Record that you have seen this alarm" onClick={() => void ack()}>Acknowledge</Button>
      <Show when={err()}>{(e) => <ActionFailure what="acknowledge" detail={e()} onRetry={() => void ack()} />}</Show>
    </Show>
  );
}
