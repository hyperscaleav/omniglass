import { Show, createSignal } from "solid-js";
import { useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import { acknowledgeAlarm, componentAlarmsKey } from "../lib/alarms";
import { FLEET_VIEW_KEY } from "../lib/fleet";
import { can, useMe } from "../lib/auth";
import { describeError } from "../lib/format";

// AcknowledgeButton (#872): "I have seen this", wherever an alarm is read as
// the reason. Acknowledging is gated on its own permission, never on the
// component's update (ADR-0109): it records that the reader looked, writes
// none of the component's data, and recomputes no health. It renders nothing
// for an alarm already acknowledged or a caller who may not.
export default function AcknowledgeButton(props: { component: string; alarm: { id: string; acknowledged?: boolean } }) {
  const me = useMe();
  const qc = useQueryClient();
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);
  const ack = async () => {
    setBusy(true);
    setErr(null);
    try {
      await acknowledgeAlarm(props.component, props.alarm.id);
      await Promise.all([qc.invalidateQueries({ queryKey: [...componentAlarmsKey(props.component)] }), qc.invalidateQueries({ queryKey: [...FLEET_VIEW_KEY] })]);
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Show when={!props.alarm.acknowledged && can(me.data, "alarm", "acknowledge")}>
      <Button size="xs" intent="quiet" loading={busy()} title={err() ?? "Record that you have seen this alarm"} onClick={() => void ack()}>Acknowledge</Button>
    </Show>
  );
}
