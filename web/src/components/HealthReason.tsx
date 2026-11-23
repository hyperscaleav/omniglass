import { Show, type JSX } from "solid-js";

// HealthReason (#2459): the verdict line's reason, "degraded: 1 member outside a
// slot has an open alarm (amp-1)". The server words it (health.SystemReason,
// from the same inputs as the health), so the console never recomputes why: it
// only says what the health read says. Nothing when the read carries no reason
// (a location's report, or an older server).
export default function HealthReason(props: { health?: string; reason?: string }): JSX.Element {
  return (
    <Show when={props.health && props.reason}>
      <span data-testid="health-reason" class="text-base-content/70">
        {props.health}: {props.reason}
      </span>
    </Show>
  );
}
