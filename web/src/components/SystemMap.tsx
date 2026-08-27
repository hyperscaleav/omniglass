import { For, Show } from "solid-js";
import { useNavigate } from "@solidjs/router";
import type { MapMarker, StandardMapDecl } from "../lib/system_map";
import Eyebrow from "./Eyebrow";

// The room, top-down (#791, ADR-0128): the standard's declared positions
// rendered at their normalized coordinates, each marker the join of "where
// this position sits" and "who holds it". Percent positioning against an
// aspect-ratio box, so the same declaration renders at any width and jsdom
// can assert it without layout.
export default function SystemMap(props: { decl: StandardMapDecl; markers: MapMarker[]; onOpen?: (componentId: string) => void }) {
  const navigate = useNavigate();
  return (
    <div class="flex flex-col gap-2 p-4">
      <Eyebrow
        label="Map"
        hint="The standard declares where each role position sits in the room; every system built to it renders the same map. A solid marker is the component holding that position; a hollow one is a position nobody staffed. Click a component to open it."
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
            const label = () => {
              const many = props.markers.filter((o) => o.role === m.role).length > 1;
              return `${m.roleLabel}${many ? ` ${m.position}` : ""} · ${m.occupant ? m.occupant.name : "empty"}`;
            };
            return (
              <Show
                when={m.occupant}
                fallback={
                  <span
                    data-testid={`mapmarker-${m.role}-${m.position}`}
                    class="absolute flex -translate-x-1/2 -translate-y-1/2 items-center gap-1.5"
                    style={{ left: `${m.x * 100}%`, top: `${m.y * 100}%` }}
                  >
                    <span class="h-2.5 w-2.5 flex-none rounded-full border border-dashed border-base-content/40" />
                    <span class="whitespace-nowrap text-[11px] text-base-content/50">{label()}</span>
                  </span>
                }
              >
                {(occ) => (
                  <button
                    type="button"
                    data-testid={`mapmarker-${m.role}-${m.position}`}
                    class="absolute flex -translate-x-1/2 -translate-y-1/2 cursor-pointer items-center gap-1.5 rounded px-1 py-0.5 hover:bg-base-content/10"
                    style={{ left: `${m.x * 100}%`, top: `${m.y * 100}%` }}
                    onClick={() => (props.onOpen ? props.onOpen(occ().componentId) : navigate(`/components/${occ().componentId}`))}
                  >
                    <span class="h-2.5 w-2.5 flex-none rounded-full" classList={{ "bg-error": occ().down, "bg-success": !occ().down }} />
                    <span class="whitespace-nowrap text-[11px]" classList={{ "text-error": occ().down }}>{label()}</span>
                  </button>
                )}
              </Show>
            );
          }}
        </For>
      </div>
    </div>
  );
}
