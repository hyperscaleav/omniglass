import type { JSX } from "solid-js";

// ActionFailure (#2441): how an alarm control reports a failed write. One short
// line, floated under the AlarmActions group (absolute, so a failure never
// stretches its row or pushes a sibling off the card), with Retry. The raw
// problem detail rides in the tooltip, never in the row.
export default function ActionFailure(props: { what: string; detail: string; onRetry: () => void }): JSX.Element {
  return (
    <span
      role="alert"
      title={props.detail}
      class="absolute right-0 top-full z-10 mt-0.5 flex max-w-64 items-center gap-1.5 whitespace-nowrap rounded bg-base-100 px-1.5 py-0.5 text-[11px] text-error shadow"
    >
      <span class="min-w-0 truncate">Could not {props.what}.</span>
      <button type="button" class="link shrink-0 font-semibold" onClick={() => props.onRetry()}>Retry</button>
    </span>
  );
}
