import { createSignal, For, Show, type JSX } from "solid-js";
import { A } from "@solidjs/router";
import InfoTip from "./InfoTip";
import KeyedFor from "./KeyedFor";
import { ArrowRight } from "./icons";
import {
  NOT_REPORTED,
  componentFigure,
  endToEndFigure,
  hopFigure,
  lossPercent,
  lossTinted,
  pipelineKinds,
  type PipelineKindChoice,
  type PlatformPipeline,
  type PlatformPipelineFigure,
  type PlatformPipelineHop,
} from "../lib/platformhealth";

// Where work is lost (#1646): the path an item takes from its run to the store,
// drawn as the hops the view returns, in the order an item crosses them. Each
// hop is one reading of the same three keys every hop records (in, out, and
// the loss ratio), so the operator learns the rule once and reads it anywhere:
// loss is (in - out) / in over summed counts, 0 is healthy, and nothing in is
// no ratio at all. Selecting a hop opens its explainer: what it does, what
// counts as out and as lost there, the keys it is read from (with their catalog
// descriptions), the class of platform component that records it, and each
// component's share, linking to where its series live.

const RATIO_RULE =
  "Each hop's loss is (in - out) / in over the hour's summed counts, never an average of ratios: the share of what it took in that it did not hand on. 0% is healthy; a hop that took nothing in has no ratio.";
const END_TO_END_RULE =
  "The share lost anywhere on the path: 1 minus the product of every hop's survival (1 - loss), so two hops losing 1% each lose 1.99% end to end, not 2%.";
const KIND_LABEL: Record<string, string> = { metric: "Metric", property: "Property", event: "Event", log: "Log" };
const CLASS_TEXT: Record<string, string> = {
  node: "every node's platform component",
  server: "every server's platform component",
};

export default function PipelineFlow(props: { pipeline: PlatformPipeline }): JSX.Element {
  const [kind, setKind] = createSignal<PipelineKindChoice>("all");
  const [selected, setSelected] = createSignal<string | undefined>();
  const hops = () => props.pipeline.hops ?? [];
  const picked = () => hops().find((h) => h.id === selected());
  const choices = (): PipelineKindChoice[] => ["all", ...pipelineKinds(props.pipeline)];
  const end = () => endToEndFigure(props.pipeline, kind());

  return (
    <section class="card border border-base-300 bg-base-200" data-testid="pipeline">
      <div class="card-body gap-4 p-5">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div class="flex items-center gap-1.5">
            <h2 class="eyebrow">Where work is lost</h2>
            <InfoTip label="the loss ratio" text={RATIO_RULE} />
          </div>
          <div role="group" aria-label="Kind" class="tabs tabs-box tabs-sm w-fit">
            <For each={choices()}>
              {(k) => (
                <button type="button" class="tab" classList={{ "tab-active": kind() === k }} aria-pressed={kind() === k} onClick={() => setKind(k)}>
                  {k === "all" ? "All" : KIND_LABEL[k]}
                </button>
              )}
            </For>
          </div>
        </div>

        <div class="flex flex-wrap items-center gap-2">
          <ol aria-label="Pipeline hops" class="flex flex-wrap items-stretch gap-2">
            <KeyedFor each={hops()} by={(h) => h.id}>
              {(h, i) => (
                <li class="flex items-center gap-2">
                  <Show when={i() > 0}>
                    <span class="text-base-content/30" aria-hidden="true"><ArrowRight size={14} /></span>
                  </Show>
                  <HopButton hop={h()} kind={kind()} pressed={selected() === h().id} onSelect={() => setSelected(selected() === h().id ? undefined : h().id)} />
                </li>
              )}
            </KeyedFor>
          </ol>
          <div class="ml-auto flex items-center gap-1.5 text-sm">
            <span class="text-base-content/60">End to end</span>
            <InfoTip label="end to end" text={END_TO_END_RULE} />
            <LossFigure ratio={end()} absent={end() === undefined} testid="pipeline-end-to-end" />
          </div>
        </div>

        <Show when={picked()} fallback={<p class="text-xs text-base-content/50">Select a hop to see what counts as lost there, the keys it is read from, and the components that record it.</p>}>
          {(h) => <HopDetail hop={h()} pipeline={props.pipeline} kind={kind()} />}
        </Show>
      </div>
    </section>
  );
}

function HopButton(props: { hop: PlatformPipelineHop; kind: PipelineKindChoice; pressed: boolean; onSelect: () => void }) {
  const f = () => hopFigure(props.hop, props.kind);
  return (
    <button
      type="button"
      data-testid={`pipeline-hop-${props.hop.id}`}
      data-hop={props.hop.id}
      aria-pressed={props.pressed}
      class="flex min-w-36 cursor-pointer flex-col gap-1 rounded-box border bg-base-100 px-3 py-2 text-left transition-colors hover:border-base-content/30 focus-visible:outline-2 focus-visible:outline-primary"
      classList={{ "border-primary": props.pressed, "border-base-300": !props.pressed }}
      onClick={() => props.onSelect()}
    >
      <span class="text-sm font-medium">{props.hop.label}</span>
      <Figures figure={f()} />
    </button>
  );
}

// Figures is in, out, and the ratio beside them. A kind the hop carried
// nothing of reads not reported; one it carried but took nothing in of shows
// its zero counts and no ratio.
function Figures(props: { figure: PlatformPipelineFigure | undefined }) {
  return (
    <span class="flex items-baseline gap-2 text-xs">
      <Show when={props.figure}>
        {(f) => (
          <span class="tnum text-base-content/60">
            <span data-testid="hop-in">{f().in}</span> in · <span data-testid="hop-out">{f().out}</span> out
          </span>
        )}
      </Show>
      <LossFigure ratio={props.figure?.loss} absent={!props.figure} testid="hop-loss" />
    </span>
  );
}

function LossFigure(props: { ratio: number | undefined; absent: boolean; testid: string }) {
  const tinted = () => lossTinted(props.ratio);
  const text = () => (props.absent ? NOT_REPORTED : props.ratio === undefined ? "nothing in" : lossPercent(props.ratio));
  return (
    <span
      data-testid={props.testid}
      data-tinted={String(tinted())}
      class="tnum font-semibold"
      classList={{ "text-error": tinted(), "font-normal text-base-content/40": props.ratio === undefined }}
    >
      {text()}
    </span>
  );
}

function HopDetail(props: { hop: PlatformPipelineHop; pipeline: PlatformPipeline; kind: PipelineKindChoice }) {
  const keyFacts = () => {
    const byName = new Map((props.pipeline.keys ?? []).map((k) => [k.name, k]));
    return (props.hop.keys ?? []).map((name) => ({ name, description: byName.get(name)?.description ?? "" }));
  };
  return (
    <div data-testid="pipeline-hop-detail" class="grid gap-4 rounded-box border border-base-300 bg-base-100 p-4 text-sm md:grid-cols-2">
      <div class="flex flex-col gap-2">
        <div class="font-medium">{props.hop.label}</div>
        <p class="text-base-content/70">{props.hop.description}</p>
        <p class="text-xs text-base-content/60">Recorded by {CLASS_TEXT[props.hop.component_class] ?? props.hop.component_class}, one series per kind (the instance), read from these keys:</p>
        <ul class="flex flex-col gap-1 text-xs">
          <For each={keyFacts()}>
            {(k) => (
              <li class="flex items-center gap-1.5">
                <code class="font-mono">{k.name}</code>
                <Show when={k.description}>
                  <span class="text-base-content/60">{k.description}</span>
                </Show>
              </li>
            )}
          </For>
        </ul>
      </div>
      <div class="flex flex-col gap-2">
        <div class="eyebrow">By component</div>
        <ul class="flex flex-col gap-1.5">
          <KeyedFor each={props.hop.by_component} by={(c) => c.component} fallback={<li class="text-xs text-base-content/50">No component has recorded this hop yet.</li>}>
            {(c) => (
              <li data-testid={`pipeline-component-${c().name}`} class="flex flex-wrap items-baseline justify-between gap-2">
                <A href={`/components/${c().component}`} class="font-medium hover:underline" title="Open the component, where each delivery series lives">{c().name}</A>
                <Figures figure={componentFigure(c(), props.kind)} />
              </li>
            )}
          </KeyedFor>
        </ul>
      </div>
    </div>
  );
}
