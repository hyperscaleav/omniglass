import { Show, type JSX } from "solid-js";
import Button from "./Button";
import DeniedPage from "./DeniedPage";
import { isDenied } from "../api/client";

// PageGate (#2442): the states a whole page passes through before its body, the
// page-sized sibling of DetailGate. A first read in flight holds a spinner, a
// refusal (403) renders the denied face (never a spinner and never Retry: asking
// again gets the same answer), any other failure says so and offers Retry, and a
// landed read renders the body. Pass the page's primary query; the retry policy
// that stops a 403 from being retried lives in lib/queryDefaults.
type GateQuery = { isPending: boolean; isError: boolean; error: unknown; refetch?: () => unknown };

export default function PageGate(props: {
  query: GateQuery;
  section: string;
  needs?: string[];
  loading?: string;
  children: JSX.Element;
}) {
  return (
    <Show when={!(props.query.isError && isDenied(props.query.error))} fallback={<DeniedPage section={props.section} needs={props.needs} />}>
      <Show
        when={!props.query.isPending}
        fallback={
          <div data-testid="page-pending" class="flex items-center gap-2 text-sm text-base-content/60">
            <span class="loading loading-spinner loading-sm" /> {props.loading ?? `Loading ${props.section.toLowerCase()}.`}
          </div>
        }
      >
        <Show
          when={!props.query.isError}
          fallback={
            <div role="alert" class="alert alert-error alert-soft text-sm">
              <span class="flex-1">{props.section} could not be loaded.</span>
              <Show when={props.query.refetch}>
                <Button size="xs" onClick={() => props.query.refetch?.()}>Retry</Button>
              </Show>
            </div>
          }
        >
          {props.children}
        </Show>
      </Show>
    </Show>
  );
}
