import { Show } from "solid-js";
import Button from "./Button";
import AddComponentLink from "./AddComponentLink";
import { can, useMe } from "../lib/auth";

// What a system with no components says (#2464): not an empty table but the
// idea (a system is what Omniglass monitors; a slot is a role in it a
// component fills) and the two ways to start. Add component opens the
// guided add-a-component flow (#2460) with this system filled in. Start from a standard is offered only
// while the system has none: the standard is what declares its slots.
export default function SystemEmpty(props: { systemId: string; hasStandard: boolean; onStandard: () => void }) {
  const me = useMe();
  const mayAdd = () => can(me.data, "component", "create") && can(me.data, "endpoint", "create");
  return (
    <div data-testid="system-empty" class="flex flex-col gap-3 rounded-box border border-dashed border-base-300 bg-base-100 p-5">
      <h3 class="text-sm font-semibold">No components yet</h3>
      <p class="max-w-prose text-[13px] text-base-content/70">
        A system is the thing Omniglass monitors: a room's conference setup, a signage wall. Its health comes from the components that serve it.
        Each component fills a <strong>slot</strong>, a role the system needs (a main display, a codec), and the system reads healthy while its slots are staffed.
      </p>
      <Show when={mayAdd()} fallback={<p class="text-xs text-base-content/60">Adding a component needs <code>component:create</code> and <code>endpoint:create</code>.</p>}>
        <div class="flex flex-wrap gap-2">
          <AddComponentLink system={props.systemId} intent="action" label="Add component" />
          <Show when={!props.hasStandard}>
            <Button onClick={props.onStandard}>Start from a standard</Button>
          </Show>
        </div>
      </Show>
    </div>
  );
}
