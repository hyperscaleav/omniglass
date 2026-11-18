import { Show } from "solid-js";
import Button from "./Button";
import { Bot } from "./icons";
import { can, useMe } from "../lib/auth";
import { alarmSubject, requestTriage } from "../lib/handover";

type Troubleshootable = { component: string; raised_by_rule?: string | null; raised_at: string; message?: string };

// TroubleshootButton (#2164): one click asks omnibot to triage this alarm. The
// shell opens the panel on the request and the panel runs the triage-alarm
// skill on it, the page going with it as context. Omnibot is handed the alarm
// by its component's name and rule (alarmSubject), never its message, and
// reads the rest itself under the person's own grants. Shown only to a person
// who may ask omnibot.
export default function TroubleshootButton(props: { alarm: Troubleshootable; size?: "xs" | "sm" }) {
  const me = useMe();
  return (
    <Show when={can(me.data, "assistant", "use")}>
      <Button
        square
        size={props.size ?? "xs"}
        icon={Bot}
        label={props.alarm.message ? `Troubleshoot ${props.alarm.message} with omnibot` : "Troubleshoot with omnibot"}
        title="Troubleshoot with omnibot: it reads this component, its alarms and its reachability, then says where the fault is"
        onClick={() => requestTriage(alarmSubject(props.alarm))}
      />
    </Show>
  );
}
