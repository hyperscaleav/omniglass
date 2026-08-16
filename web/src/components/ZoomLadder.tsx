import { For, Show } from "solid-js";
import Button from "./Button";
import type { ZoomChip } from "../lib/zoom";

// The four-chip zoom ladder (#633, design pass): a stepper (fleet -> location
// -> system -> component) with the count in a monospace tail, an arrow
// between steps, and an optional per-zoom hint at the right. Pure
// presentation over zoomChips' output; the resolution rules live in lib/zoom.
export default function ZoomLadder(props: { chips: ZoomChip[]; hint?: string; onSelect?: (chip: ZoomChip) => void }) {
  return (
    <div class="flex items-center gap-2">
      <span class="text-[10px] uppercase tracking-wider text-base-content/40">Zoom</span>
      <div data-testid="zoom-ladder" class="flex items-center gap-1">
        <For each={props.chips}>
          {(chip, i) => (
            <>
              <Show when={i() > 0}>
                <span class="text-base-content/30">→</span>
              </Show>
              <Button intent={chip.active ? "action" : "quiet"} size="xs" disabled={!chip.enabled} onClick={() => props.onSelect?.(chip)}>
                {chip.label}
                <Show when={chip.count !== ""}>
                  <span class="ml-1 font-mono text-[10px] opacity-60">{chip.count}</span>
                </Show>
              </Button>
            </>
          )}
        </For>
      </div>
      <Show when={props.hint}>
        <span class="ml-auto text-xs text-base-content/40">{props.hint}</span>
      </Show>
    </div>
  );
}
