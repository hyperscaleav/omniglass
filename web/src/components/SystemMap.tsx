import { createMemo, createSignal, Index, onCleanup, onMount, Show } from "solid-js";
import { Dynamic } from "solid-js/web";
import { useNavigate } from "@solidjs/router";
import Tooltip from "./ui/Tooltip";
import { fitMarkers, layoutMarkers, type MapMarker, type Rect, type StandardMapDecl } from "../lib/system_map";
import { healthHue } from "../lib/health";
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
// health's hue, the label under it, read through the same member context the
// rows use, so the map and the rows never disagree. The detail (slot,
// product, health, the alarm behind a red one) rides a tooltip, never the
// map itself. An empty position is a dashed ring named for its slot.

// How a marker hangs off its point: the icon's centre sits on the point (the
// offsets are half the 18px icon plus the marker's px-1 / py-0.5 padding), and
// near an edge the marker anchors inward (lib/system_map's markerAnchor). A
// label wraps to two lines before it truncates, so "Wireless Microphone 3"
// and "4" stay distinct; anything longer ellipsizes, its full text in the
// tooltip.
//
// Which anchor, and whether a label shows at all, is decided in pixels
// (#1072): the map's rendered width feeds lib/system_map's layoutMarkers, a
// pure pass over the whole marker list that keeps each label inside the box
// and off every other label and icon, moving it to its other side or, when
// nothing is clear, collapsing it to the icon alone (its text stays in the
// tooltip and the accessible name). The geometry lives in that pass,
// so jsdom and a browser lay out identically.
const ANCHOR_X = {
  start: "-translate-x-[13px] items-start text-left",
  center: "-translate-x-1/2 items-center text-center",
  end: "translate-x-[calc(-100%+13px)] items-end text-right",
} as const;
const ANCHOR_Y = { below: "flex-col -translate-y-[11px]", above: "flex-col-reverse translate-y-[calc(-100%+11px)]" } as const;
// Before the box is measured (first paint, jsdom): the width its max-w-2xl cap gives it.
const DEFAULT_WIDTH = 672;
const box = (r: Rect) => [r.x, r.y, r.w, r.h].map((n) => Math.round(n * 10) / 10).join(" ");
const MARKER = "rounded px-1 py-0.5 outline-none focus-visible:shadow-[inset_0_0_0_1px_var(--color-primary)]";

export default function SystemMap(props: {
  decl: StandardMapDecl;
  markers: MapMarker[];
  member?: MemberContext;
  onOpen?: (componentId: string) => void;
}) {
  const navigate = useNavigate();
  const [width, setWidth] = createSignal(DEFAULT_WIDTH);
  let mapEl!: HTMLDivElement;
  onMount(() => {
    if (mapEl.clientWidth > 0) setWidth(mapEl.clientWidth);
    // jsdom has no ResizeObserver; there the default width stands.
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver((entries) => {
      const w = entries[0]?.contentRect.width ?? 0;
      if (w > 0) setWidth(w);
    });
    ro.observe(mapEl);
    onCleanup(() => ro.disconnect());
  });
  // The position number renders only when the slot declares more than one
  // position: "Room Microphone 2", but never "Display 1".
  const slotOf = (m: MapMarker) => {
    const many = props.markers.filter((o) => o.slot === m.slot).length > 1;
    return `${m.slotLabel}${many ? ` ${m.position}` : ""}`;
  };
  const labelOf = (m: MapMarker) =>
    m.occupant ? (props.member?.label(m.occupant.componentId, m.occupant.name) ?? m.occupant.name) : slotOf(m);
  // The box is cropped to what it holds, so a system with a few members is a
  // small map rather than a mostly empty room (#2465). Markers below are the
  // cropped ones; slotOf and labelOf only read slot and occupant, which a crop
  // does not touch.
  const fit = createMemo(() => fitMarkers(props.markers, props.decl.aspect));
  const placements = createMemo(() =>
    layoutMarkers(
      fit().markers.map((m, i) => ({ x: m.x, y: m.y, text: labelOf(props.markers[i]) })),
      { w: width(), h: width() / fit().aspect },
    ),
  );
  return (
    <div class="flex flex-col gap-2 p-4">
      <Eyebrow
        label="Map"
        hint="The standard declares where each slot position sits in the room; every system built to it renders the same map. Each marker is the component holding that position, its icon in its own health colour; a dashed ring is a position nobody staffed. Hover or focus a marker for its slot and health; click it to open the component."
      />
      <div
        ref={mapEl}
        data-testid="system-map"
        class="relative w-full rounded-box border border-base-300 bg-base-200"
        style={{ "aspect-ratio": String(fit().aspect), "max-width": `${Math.max(fit().width * 42, 18).toFixed(2)}rem` }}
      >
        {/* Index, not For: markers are keyed by their slot in the declaration,
            so a health poll that rebuilds the marker objects updates each
            marker in place instead of remounting it (which would drop focus
            and close an open tooltip every few seconds). */}
        <Index each={fit().markers}>
          {(m, i) => {
            const slot = () => slotOf(m());
            const placed = () => placements()[i];
            const place = () => {
              const a = placed().anchor;
              return `absolute flex gap-0.5 ${MARKER} ${ANCHOR_X[a.x]} ${ANCHOR_Y[a.y]}`;
            };
            const at = () => ({ left: `${m().x * 100}%`, top: `${m().y * 100}%` });
            const testid = () => `mapmarker-${m().slot}-${m().position}`;
            return (
              <Show
                when={m().occupant}
                fallback={
                  <Tooltip
                    as="span"
                    tabIndex={0}
                    data-testid={testid()}
                    data-empty
                    aria-label={slot()}
                    class={`${place()} cursor-default`}
                    style={at()}
                    contentClass="z-100 max-w-56 rounded-box border border-base-300 bg-base-100 px-2.5 py-1.5 text-xs leading-snug shadow-lg"
                    content={<>{slot()}: nobody staffs this position.</>}
                  >
                    <span data-glyph class={`inline-flex ${healthHue(null)}`}>
                      <CircleDashed size={18} />
                    </span>
                    <Show when={placed().label}>
                      {(b) => (
                        <span data-maplabel data-label-box={box(b())} class="line-clamp-2 w-max max-w-28 text-balance text-[11px] leading-tight text-base-content/50">{slot()}</span>
                      )}
                    </Show>
                  </Tooltip>
                }
              >
                {(occ) => {
                  const id = () => occ().componentId;
                  const memberHealth = () => props.member?.health(id()) ?? null;
                  const label = () => props.member?.label(id(), occ().name) ?? occ().name;
                  const product = () => props.member?.product(id()) ?? "";
                  const alarm = () => props.member?.alarm(id());
                  const health = () => memberHealth() ?? (occ().down ? "outage" : "unknown");
                  return (
                    <Tooltip
                      data-testid={testid()}
                      aria-label={`${label()}, ${slot()}, ${health()}`}
                      class={`${place()} cursor-pointer hover:bg-base-content/10`}
                      style={at()}
                      onClick={() => (props.onOpen ? props.onOpen(id()) : navigate(`/components/${id()}`))}
                      contentTestId="mapmarker-detail"
                      contentClass="z-100 flex max-w-64 flex-col gap-0.5 rounded-box border border-base-300 bg-base-100 px-2.5 py-1.5 text-xs leading-snug shadow-lg"
                      content={
                        <>
                          <span class="font-medium">{label()}</span>
                          <span class="text-base-content/70">{slot()}</span>
                          <Show when={product()}>
                            <span class="text-base-content/70">{product()}</span>
                          </Show>
                          <span class={healthHue(memberHealth(), occ().down)}>{health()}</span>
                          <Show when={alarm()}>
                            <span class="text-error">{alarm()}</span>
                          </Show>
                        </>
                      }
                    >
                      <span data-glyph class={`inline-flex ${healthHue(memberHealth(), occ().down)}`}>
                        <Dynamic component={resolveIcon(props.member?.icon(id()))} size={18} />
                      </span>
                      <Show when={placed().label}>
                        {(b) => (
                          <span data-maplabel data-label-box={box(b())} class="line-clamp-2 w-max max-w-28 text-balance font-data text-[11px] leading-tight">{label()}</span>
                        )}
                      </Show>
                    </Tooltip>
                  );
                }}
              </Show>
            );
          }}
        </Index>
      </div>
    </div>
  );
}
