import { Show } from "solid-js";
import { Layers, ArrowUpRight } from "./icons";

// The single placeholder skeleton every not-yet-built section shares. The top bar
// already labels the section, so there is no page heading or subtitle here (the
// live pages drop those too); only the section name, a "soon" marker, what it will
// do, and a tracking-issue link when one exists. Every stub renders exactly this,
// varying only title + hint (+ issue).
export default function Placeholder(props: { title: string; hint: string; issue?: number }) {
  return (
    <div class="flex min-h-[58vh] items-center justify-center">
      <div class="flex max-w-md flex-col items-center gap-3 text-center">
        <span class="text-base-content/20"><Layers size={34} /></span>
        <div class="flex items-center gap-2">
          <span class="text-lg font-semibold">{props.title}</span>
          <span class="rounded bg-base-content/5 px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-base-content/40">soon</span>
        </div>
        <p class="text-sm text-base-content/55">{props.hint}</p>
        <Show when={props.issue}>
          <a
            class="link inline-flex items-center gap-1 text-xs text-base-content/40"
            href={`https://github.com/hyperscaleav/omniglass/issues/${props.issue}`}
            target="_blank"
            rel="noreferrer"
          >
            #{props.issue} <ArrowUpRight size={12} />
          </a>
        </Show>
      </div>
    </div>
  );
}
