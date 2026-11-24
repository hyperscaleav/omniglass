import { Show, type JSX } from "solid-js";
import { CLOCK_TICK_MS, createClock } from "../lib/clock";
import { alarmDurationText } from "../lib/alarms";
import Tooltip from "./ui/Tooltip";

type Timed = { raised_at: string; cleared_at?: string | null };

// AlarmDuration (#2426): how long an alarm was raised. A cleared alarm reads
// raised to cleared, fixed, and starts no timer; an open one reads raised to
// now and ticks, so an operator watching a page sees the age grow. Only an open
// alarm owns a clock, so a long history of cleared rows costs nothing per
// second. One formatter (durationText, via alarmDurationText) for every surface.
export default function AlarmDuration(props: { alarm: Timed; class?: string }): JSX.Element {
  return (
    <Show when={!props.alarm.cleared_at} fallback={<Span alarm={props.alarm} class={props.class} open={false} now={() => Date.now()} />}>
      <Live alarm={props.alarm} class={props.class} />
    </Show>
  );
}

function Live(props: { alarm: Timed; class?: string }): JSX.Element {
  const now = createClock(CLOCK_TICK_MS);
  return <Span alarm={props.alarm} class={props.class} open={true} now={now} />;
}

function Span(props: { alarm: Timed; class?: string; open: boolean; now: () => number }): JSX.Element {
  return (
    <Tooltip as="span" tabIndex={0}
      data-testid="alarm-duration"
      data-open={props.open ? "true" : "false"}
      class={`inline-block whitespace-nowrap tnum ${props.class ?? ""}`}
      content={props.open ? "Still raised: this grows until it clears" : "Raised to cleared"}
    >
      {alarmDurationText(props.alarm, props.now())}
    </Tooltip>
  );
}
