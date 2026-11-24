import { Index, Match, Show, Switch } from "solid-js";
import OmnibotStep from "./OmnibotStep";
import { ChevronRight, TriangleAlert } from "./icons";
import { stepFailed, thoughtSummary, type Reasoning, type Step, type Thought } from "../lib/assistant";

// as narrows a part to one kind, or undefined, for a Match's when.
function as<K extends "reasoning" | "step">(p: Reasoning | Step, kind: K): Extract<Reasoning | Step, { kind: K }> | undefined {
  return p.kind === kind ? (p as Extract<Reasoning | Step, { kind: K }>) : undefined;
}

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
        <Index each={props.thought.parts}>
          {(part) => (
            <Switch>
              <Match when={as(part(), "reasoning")}>{(r) => <div class="whitespace-pre-wrap">{r().text}</div>}</Match>
              <Match when={as(part(), "step")}>{(s) => <OmnibotStep step={s()} waiting={props.waitingId === s().id} />}</Match>
            </Switch>
          )}
        </Index>
      </div>
    </details>
  );
}
