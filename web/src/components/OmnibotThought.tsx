import { For, Match, Show, Switch } from "solid-js";
import OmnibotStep from "./OmnibotStep";
import { ChevronRight, TriangleAlert } from "./icons";
import { stepFailed, thoughtSummary, type Thought } from "../lib/assistant";

// OmnibotThought is the reasoning and tool calls of one turn, folded into one
// collapsed block (#2422) so the answer is what the eye lands on. Collapsed it
// is a single line: what is happening now while the run is live, the count of
// calls once it settles, and a flag if any failed. Expanded, the reasoning
// text and the call rows sit in the order they happened.
export default function OmnibotThought(props: { thought: Thought; live: boolean; waitingId?: string }) {
  const failed = () => props.thought.parts.some((p) => p.kind === "step" && stepFailed(p));
  return (
    <details data-testid="omnibot-thinking" data-live={props.live ? "true" : undefined} class="group text-xs text-base-content/60">
      <summary class="flex cursor-pointer select-none items-center gap-1.5">
        <span class="shrink-0 transition-transform group-open:rotate-90">
          <ChevronRight size={12} />
        </span>
        <Show when={props.live}>
          <span class="loading loading-dots loading-xs" />
        </Show>
        <Show when={failed()}>
          <span class="shrink-0 text-error" data-testid="omnibot-thinking-error">
            <TriangleAlert size={12} />
          </span>
        </Show>
        <span data-testid="omnibot-thinking-summary" class={failed() && !props.live ? "text-error" : undefined}>
          {thoughtSummary(props.thought, props.live, props.waitingId)}
        </span>
      </summary>
      <div class="mt-1 flex flex-col gap-1.5 border-l-2 border-base-300 pl-2">
        <For each={props.thought.parts}>
          {(p) => (
            <Switch>
              <Match when={p.kind === "reasoning" && p}>{(r) => <div class="whitespace-pre-wrap">{r().text}</div>}</Match>
              <Match when={p.kind === "step" && p}>{(s) => <OmnibotStep step={s()} waiting={props.waitingId === s().id} />}</Match>
            </Switch>
          )}
        </For>
      </div>
    </details>
  );
}
