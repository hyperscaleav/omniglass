import { For, Show, type JSX } from "solid-js";
import { A } from "@solidjs/router";
import type { HealthMemberBody } from "../api/api.gen";

// OutsideSlotNotice (#2459): the members of a system that fill no slot, said
// plainly with what that means for health and the way to change it. A member
// outside every slot is tracked and its alarms show on the system, but it does
// not count toward the system's health: health comes only from rules someone
// declared (the slots and their impact today), never from an alarm merely
// existing (ADR-2459). The action opens the system's Configure tab, where a slot
// is declared and filled. placeHref is omitted for a caller who may not
// configure the system.
export default function OutsideSlotNotice(props: { members?: HealthMemberBody[]; placeHref?: string }): JSX.Element {
  const outside = () => (props.members ?? []).filter((m) => m.outside_slot);
  const alarmed = () => outside().filter((m) => (m.alarms ?? []).length > 0).length;
  return (
    <Show when={outside().length > 0}>
      <div data-testid="outside-slot-notice" class="flex flex-wrap items-center gap-x-2 gap-y-1 rounded-box border border-base-300 bg-base-100 px-3 py-2 text-sm">
        <span class="text-base-content/80">
          {outside().length === 1 ? "1 component is" : `${outside().length} components are`} not in a slot:{" "}
          <For each={outside()}>
            {(m, i) => (
              <>
                <span class="font-mono text-xs">{m.name}</span>
                {i() < outside().length - 1 ? ", " : ""}
              </>
            )}
          </For>
          .
        </span>
        <span class="text-base-content/60">
          {outside().length === 1
            ? "Its alarms show here, but it does not count toward this system's health until placed in a slot."
            : "Their alarms show here, but they do not count toward this system's health until placed in a slot."}{" "}
          A system's health comes only from rules someone declared: its slots, and what each slot says losing it costs.
          <Show when={alarmed() > 0}> {alarmed() === 1 ? (outside().length === 1 ? "It has" : "1 of them has") : `${alarmed()} of them have`} an open alarm now.</Show>
        </span>
        <Show when={props.placeHref}>
          {(href) => (
            <A href={href()} data-testid="place-in-slot" class="btn btn-quiet btn-xs ml-auto">
              Place in a slot
            </A>
          )}
        </Show>
      </div>
    </Show>
  );
}
