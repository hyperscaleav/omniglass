import { For, Show } from "solid-js";
import { Dynamic } from "solid-js/web";
import { useNavigate } from "@solidjs/router";
import { Tooltip } from "@kobalte/core/tooltip";
import { markerAnchor, type MapMarker, type StandardMapDecl } from "../lib/system_map";
import { verdictHue } from "../lib/health";
import type { MemberContext } from "../lib/detail";
import { CircleDashed, resolveIcon } from "./icons";
import Eyebrow from "./Eyebrow";

// The room, top-down (#791, ADR-0128): the standard's declared positions
// rendered at their normalized coordinates, each marker the join of "where
// this position sits" and "who holds it". Percent positioning against an
// aspect-ratio box, so the same declaration renders at any width and jsdom
// can assert it without layout.
//
// A marker reads like its component's row (#1074): the type's icon in its own
// verdict's hue, the label under it, read through the same member context the
// rows use, so the map and the rows never disagree. The detail (role,
// product, health, the alarm behind a red one) rides a tooltip, never the
// map itself. An empty position is a dashed ring named for its role.

// How a marker hangs off its point: the icon's centre sits on the point, and
// near an edge the marker anchors inward (lib/system_map's markerAnchor).
// A label wraps to two lines rather than truncating, so "Wireless Microphone
// 3" and "4" never collapse into the same "Wireless Micropho...".
const ANCHOR_X = {
  start: "-translate-x-[9px] items-start text-left",
  center: "-translate-x-1/2 items-center text-center",
  end: "translate-x-[calc(-100%+9px)] items-end text-right",
} as const;
const ANCHOR_Y = { below: "flex-col -translate-y-[9px]", above: "flex-col-reverse translate-y-[calc(-100%+9px)]" } as const;

export default function SystemMap(props: {
  decl: StandardMapDecl;
  markers: MapMarker[];
  member?: MemberContext;
  onOpen?: (componentId: string) => void;
}) {
  const navigate = useNavigate();
  return (
    <div class="flex flex-col gap-2 p-4">
      <Eyebrow
        label="Map"
        hint="The standard declares where each role position sits in the room; every system built to it renders the same map. Each marker is the component holding that position, its icon in its own health colour; a dashed ring is a position nobody staffed. Hover a marker for its role and health, click it to open the component."
      />
      <div
        data-testid="system-map"
        class="relative w-full max-w-2xl rounded-box border border-base-300 bg-base-200"
        style={{ "aspect-ratio": String(props.decl.aspect) }}
      >
        <For each={props.markers}>
          {(m) => {
            // The position number renders only when the role declares more
            // than one position: "Room Microphone 2", but never "Display 1".
            const role = () => {
              const many = props.markers.filter((o) => o.role === m.role).length > 1;
              return `${m.roleLabel}${many ? ` ${m.position}` : ""}`;
            };
            const a = markerAnchor(m.x, m.y);
            const place = `absolute flex gap-0.5 ${ANCHOR_X[a.x]} ${ANCHOR_Y[a.y]}`;
            const at = { left: `${m.x * 100}%`, top: `${m.y * 100}%` };
            return (
              <Show
                when={m.occupant}
                fallback={
                  <Tooltip openDelay={150} closeDelay={100} placement="top" gutter={4}>
                    <Tooltip.Trigger as="span" data-testid={`mapmarker-${m.role}-${m.position}`} data-empty class={`${place} cursor-default`} style={at}>
                      <span data-glyph class={`inline-flex ${verdictHue(null)}`}>
                        <CircleDashed size={18} />
                      </span>
                      <span data-maplabel class="line-clamp-2 w-max max-w-28 text-[11px] leading-tight text-base-content/50">{role()}</span>
                    </Tooltip.Trigger>
                    <Tooltip.Portal>
                      <Tooltip.Content class="z-100 max-w-56 rounded-box border border-base-300 bg-base-100 px-2.5 py-1.5 text-xs leading-snug shadow-lg">
                        {role()}: nobody staffs this position.
                      </Tooltip.Content>
                    </Tooltip.Portal>
                  </Tooltip>
                }
              >
                {(occ) => {
                  const id = () => occ().componentId;
                  const verdict = () => props.member?.verdict(id()) ?? null;
                  const label = () => props.member?.label(id(), occ().name) ?? occ().name;
                  const product = () => props.member?.product(id()) ?? "";
                  const alarm = () => props.member?.alarm(id());
                  const health = () => verdict() ?? (occ().down ? "outage" : "unknown");
                  return (
                    <Tooltip openDelay={150} closeDelay={100} placement="top" gutter={4}>
                      <Tooltip.Trigger
                        type="button"
                        data-testid={`mapmarker-${m.role}-${m.position}`}
                        aria-label={`${label()}, ${role()}, ${health()}`}
                        class={`${place} cursor-pointer rounded px-1 py-0.5 outline-none hover:bg-base-content/10 focus-visible:shadow-[inset_0_0_0_1px_var(--color-primary)]`}
                        style={at}
                        onClick={() => (props.onOpen ? props.onOpen(id()) : navigate(`/components/${id()}`))}
                      >
                        <span data-glyph class={`inline-flex ${verdictHue(verdict(), occ().down)}`}>
                          <Dynamic component={resolveIcon(props.member?.icon(id()))} size={18} />
                        </span>
                        <span data-maplabel class="line-clamp-2 w-max max-w-28 font-data text-[11px] leading-tight">{label()}</span>
                      </Tooltip.Trigger>
                      <Tooltip.Portal>
                        <Tooltip.Content
                          data-testid="mapmarker-detail"
                          class="z-100 flex max-w-64 flex-col gap-0.5 rounded-box border border-base-300 bg-base-100 px-2.5 py-1.5 text-xs leading-snug shadow-lg"
                        >
                          <span class="font-medium">{label()}</span>
                          <span class="text-base-content/70">{role()}</span>
                          <Show when={product()}>
                            <span class="text-base-content/70">{product()}</span>
                          </Show>
                          <span class={verdictHue(verdict(), occ().down)}>{health()}</span>
                          <Show when={alarm()}>
                            <span class="text-error">{alarm()}</span>
                          </Show>
                        </Tooltip.Content>
                      </Tooltip.Portal>
                    </Tooltip>
                  );
                }}
              </Show>
            );
          }}
        </For>
      </div>
    </div>
  );
}
