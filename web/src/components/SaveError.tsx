import { Show } from "solid-js";
import type { BladeEdit } from "../lib/blades";

// SaveError: the reason a Save failed, drawn where the operator is looking, in
// the footer beside the Save button. A failed save leaves the slot editing, and
// on a long form scrolled to its last section the form's own top of page is out
// of sight, so every host of the edit slot (the blade footer, the Configure
// footer) renders this one alert from the slot's saveError.
export default function SaveError(props: { slot: BladeEdit }) {
  return (
    <Show when={props.slot.saveError()}>
      {(msg) => (
        <div role="alert" data-testid="save-error" class="alert alert-error alert-soft py-2 text-sm">
          <span>{msg()}</span>
        </div>
      )}
    </Show>
  );
}
