import { Show, type JSX } from "solid-js";
import Button from "./Button";

// DetailGate (#872): the states every detail view passes through before its
// body, in one place so the three kinds cannot drift. A miss is judged only
// once the reads are current (the create handoff lands on a new row's address
// while the cached view predates it), a read in flight holds the layout with
// a skeleton, and a failed read says why and offers Retry rather than a blank.
export default function DetailGate(props: {
  noun: string;
  missing: boolean;
  pending: boolean;
  error: string | null;
  retrying?: boolean;
  onRetry: () => void;
  children: JSX.Element;
}) {
  return (
    <Show
      when={!props.missing}
      fallback={
        <div role="alert" class="alert alert-warning alert-soft text-sm">
          <span>No {props.noun} answers this address. It may have been deleted, or the link is stale.</span>
        </div>
      }
    >
      <Show when={!props.pending} fallback={<div data-testid="detail-pending" class="skeleton h-32 w-full" />}>
        <Show
          when={!props.error}
          fallback={
            <div role="alert" class="alert alert-error alert-soft text-sm">
              <span class="flex-1">{props.error}</span>
              <Button size="xs" loading={props.retrying} onClick={() => props.onRetry()}>Retry</Button>
            </div>
          }
        >
          {props.children}
        </Show>
      </Show>
    </Show>
  );
}
