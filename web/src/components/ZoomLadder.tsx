import { For } from "solid-js";
import Button from "./Button";
import type { ZoomChip } from "../lib/zoom";

// The four-chip zoom ladder (#633): fleet, location, system, component. Pure
// presentation over zoomChips' output; the resolution rules (anchor chain,
// never a stored id) live and are tested in lib/zoom.ts.
export default function ZoomLadder(props: { chips: ZoomChip[]; onSelect?: (chip: ZoomChip) => void }) {
  return (
    <div data-testid="zoom-ladder" class="flex items-center gap-1">
      <For each={props.chips}>
        {(chip) => (
          <Button
            intent={chip.active ? "action" : "quiet"}
            size="xs"
            disabled={!chip.enabled}
            onClick={() => props.onSelect?.(chip)}
          >
            {chip.label}
            {chip.count === "" ? "" : <span class="opacity-60">{` · ${chip.count}`}</span>}
          </Button>
        )}
      </For>
    </div>
  );
}
