import { Show, type Component } from "solid-js";
import { Dynamic } from "solid-js/web";
import { ArrowUpRight, Layers } from "./icons";

// The single placeholder skeleton every not-yet-built section shares. The top bar
// already labels the section, so there is no page heading or subtitle here (the
// live pages drop those too); only the section's own icon, its name, a "soon"
// marker, what it will do, and a tracking-issue link when one exists. Every stub
// renders exactly this, varying only icon + title + hint (+ issue).
export default function Placeholder(props: { title: string; hint: string; issue?: number; icon?: Component<{ size?: number }> }) {
  const Icon = () => props.icon ?? Layers;
  return (
    <div class="flex min-h-[58vh] items-center justify-center">
      <div class="flex max-w-md flex-col items-center gap-3 text-center">
        <span class="text-base-content/20"><Dynamic component={Icon()} size={34} /></span>
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
