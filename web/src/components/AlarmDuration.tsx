import type { JSX } from "solid-js";
import { CLOCK_TICK_MS, createClock } from "../lib/clock";
import { alarmDurationText } from "../lib/alarms";

// AlarmDuration (#2426): how long an alarm was raised. A cleared alarm reads
// raised to cleared and stays put; an open one reads raised to now and ticks,
// so an operator watching a page sees the age grow. One formatter
// (durationText, via alarmDurationText) for every surface that shows it.
export default function AlarmDuration(props: { alarm: { raised_at: string; cleared_at?: string | null }; class?: string }): JSX.Element {
  const now = createClock(CLOCK_TICK_MS);
  const open = () => !props.alarm.cleared_at;
  return (
    <span
      data-testid="alarm-duration"
      data-open={open() ? "true" : "false"}
      class={`inline-block whitespace-nowrap tnum ${props.class ?? ""}`}
      title={open() ? "Still raised: this grows until it clears" : "Raised to cleared"}
    >
      {alarmDurationText(props.alarm, now())}
    </span>
  );
}
