import { Show, type JSX } from "solid-js";

// HealthReason (#2459): the verdict line's reason, "degraded: slot display is
// impaired (0 of 1 occupying)". The server words it (health.SystemReason,
// from the same inputs as the health), so the console never recomputes why: it
// only says what the health read says. Nothing for a healthy system (a calm
// fleet stays calm, ADR-0154: the badge already says it), nor when the read
// carries no reason (a location's report, or an older server).
export default function HealthReason(props: { health?: string; reason?: string }): JSX.Element {
  return (
    <Show when={props.health && props.health !== "healthy" && props.reason}>
      <span data-testid="health-reason" class="text-base-content/70">
        {props.health}: {props.reason}
      </span>
    </Show>
  );
}
