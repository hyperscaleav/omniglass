import { For, Show, type JSX } from "solid-js";
import { A } from "@solidjs/router";
import ReachLadder from "./ReachLadder";
import { entityLabel } from "../lib/entities";
import type { Step } from "../lib/assistant";
import type { Reachability } from "../lib/reachability";

// OmnibotStep is one tool call in the omnibot panel (#1167): the tool, its
// arguments, and its result. A tool in STEP_RENDERERS shows its result the way
// the console shows that thing elsewhere (a component as a link to its page,
// reachability as each endpoint's ladder); every other tool, and any result a
// renderer cannot read, gets the generic card, so a tool added to the catalog
// is never a blank. The raw result stays one click away under either.

// A renderer reads a tool's parsed result and returns its view, or null when
// the result is not the shape it reads (an error text, a changed schema).
export type StepRenderer = (result: unknown) => JSX.Element | null;

type Ref = { id: string; name: string; label?: string };

function isRef(v: unknown): v is Ref {
  const r = v as Ref;
  return !!r && typeof r === "object" && typeof r.id === "string" && typeof r.name === "string";
}

function ComponentLink(p: { c: Ref }) {
  return (
    <A href={`/components/${p.c.id}`} class="link link-hover font-medium">
      {entityLabel(p.c)}
    </A>
  );
}

export const STEP_RENDERERS: Record<string, StepRenderer> = {
  "get-component": (r) =>
    isRef(r) ? (
      <div class="flex items-baseline gap-2">
        <ComponentLink c={r} />
        <Show when={entityLabel(r) !== r.name}>
          <span class="font-mono text-xs text-base-content/60">{r.name}</span>
        </Show>
      </div>
    ) : null,
  "list-components": (r) => {
    const list = (r as { components?: unknown })?.components;
    if (!Array.isArray(list) || !list.every(isRef)) return null;
    return (
      <Show when={list.length > 0} fallback={<p class="text-sm text-base-content/60">No components.</p>}>
        <ul class="flex flex-col gap-0.5 text-sm">
          <For each={list}>{(c) => <li><ComponentLink c={c} /></li>}</For>
        </ul>
      </Show>
    );
  },
  "get-component-reachability": (r) => {
    const reach = r as Reachability;
    if (!reach || !Array.isArray(reach.endpoints)) return null;
    return (
      <Show when={reach.endpoints.length > 0} fallback={<p class="text-sm text-base-content/60">No endpoints.</p>}>
        <ul class="flex flex-col gap-1.5 text-sm">
          <For each={reach.endpoints}>
            {(e) => (
              <li class="flex flex-wrap items-center gap-2">
                <span class="font-medium">{e.label || e.endpoint}</span>
                <span class="text-xs text-base-content/60">{e.transport}{e.address ? ` ${e.address}` : ""}</span>
                <ReachLadder iface={e} />
              </li>
            )}
          </For>
        </ul>
      </Show>
    );
  },
};

function parse(result: string): unknown {
  try {
    return JSON.parse(result);
  } catch {
    return result;
  }
}

function pretty(v: unknown): string {
  return typeof v === "string" ? v : JSON.stringify(v, null, 2);
}

function Args(p: { args: Record<string, unknown> }) {
  return (
    <Show when={Object.keys(p.args).length > 0}>
      <dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-xs">
        <For each={Object.entries(p.args)}>
          {([k, v]) => (
            <>
              <dt class="text-base-content/60">{k}</dt>
              <dd class="font-mono break-all">{typeof v === "string" ? v : JSON.stringify(v)}</dd>
            </>
          )}
        </For>
      </dl>
    </Show>
  );
}

export default function OmnibotStep(props: { step: Step }) {
  const parsed = () => (props.step.result === undefined ? undefined : parse(props.step.result));
  const view = () => {
    const r = parsed();
    const render = STEP_RENDERERS[props.step.tool];
    return r === undefined || !render ? null : render(r);
  };
  return (
    <div data-testid="omnibot-step" class="rounded-box border border-base-300 bg-base-100 p-2.5 text-sm">
      <div class="mb-1.5 flex items-center gap-2">
        <span class="font-mono text-xs font-medium">{props.step.tool}</span>
        <Show when={props.step.decision === "approved"}>
          <span class="badge badge-sm badge-soft badge-success">Approved</span>
        </Show>
        <Show when={props.step.decision === "declined"}>
          <span class="badge badge-sm badge-soft badge-warning">Declined</span>
        </Show>
        <Show when={props.step.result === undefined && props.step.decision !== "declined"}>
          <span class="badge badge-sm badge-ghost">Running</span>
        </Show>
      </div>
      <Args args={props.step.args} />
      <Show when={parsed() !== undefined}>
        <div class="mt-2">{view()}</div>
        <details class="mt-1.5 text-xs" open={!view()}>
          <summary class="cursor-pointer text-base-content/60">Result</summary>
          <pre data-testid="omnibot-step-raw" class="mt-1 max-h-60 overflow-auto rounded-field bg-base-200 p-2 whitespace-pre-wrap break-all">
            {pretty(parsed())}
          </pre>
        </details>
      </Show>
    </div>
  );
}
