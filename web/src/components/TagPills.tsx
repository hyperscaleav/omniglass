import { For, Show, type JSX } from "solid-js";
import { tagHue } from "../lib/tagcolor";

// TagPills renders an entity's effective tags as a row of colored key=value
// chips, one per key, sorted for a stable order. Each chip's color is derived
// from its key (tagHue), crossing only the hue into the .tag-pill CSS recipe as
// the --tag-h custom property; the recipe (text, outline, translucent fill) and
// the per-theme lightness/chroma live in the stylesheet. An empty set renders a
// muted dash, matching the other directory cells.
export default function TagPills(props: { tags?: Record<string, string> }): JSX.Element {
  const keys = () => Object.keys(props.tags ?? {}).sort();
  return (
    <Show when={keys().length} fallback={<span class="text-base-content/40">—</span>}>
      <span class="inline-flex flex-wrap items-center gap-1">
        <For each={keys()}>
          {(k) => (
            <span class="badge badge-sm tag-pill" style={{ "--tag-h": String(tagHue(k)) }} title={`${k} = ${props.tags![k]}`}>
              <span class="font-medium">{k}</span>
              <span class="opacity-40">=</span>
              <span>{props.tags![k]}</span>
            </span>
          )}
        </For>
      </span>
    </Show>
  );
}
