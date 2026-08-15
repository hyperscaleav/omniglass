import { Show } from "solid-js";
import { fleetTotals, locationsWithoutSystems, type FleetView } from "../lib/fleet";

// The fleet zoom's right column (#633): the headline totals and the gap
// footer. A page-level aside, deliberately not a blade: BladeStack is a
// portaled overlay with a scrim for focused edits, and the inspector is a
// persistent, read-only summary that shares the page.
export default function FleetInspector(props: { view: FleetView }) {
  const totals = () => fleetTotals(props.view);
  const holes = () => locationsWithoutSystems(props.view).length;
  return (
    <aside data-testid="fleet-inspector" class="w-64 flex-none">
      <div class="sticky top-4 flex flex-col gap-4">
        <div>
          <h2 class="text-sm font-semibold">Your fleet</h2>
          <ul class="mt-2 flex flex-col gap-1 text-sm text-base-content/70">
            <li>{totals().systems === 1 ? "1 system" : `${totals().systems} systems`}</li>
            <li>{totals().components === 1 ? "1 component" : `${totals().components} components`}</li>
            <li>{totals().roots === 1 ? "1 root" : `${totals().roots} roots`}</li>
          </ul>
        </div>
        <Show when={holes() > 0}>
          <p class="text-sm text-base-content/60">
            {holes() === 1 ? "1 location has no system" : `${holes()} locations have no system`}: a dashed room is
            waiting to be commissioned.
          </p>
        </Show>
      </div>
    </aside>
  );
}
