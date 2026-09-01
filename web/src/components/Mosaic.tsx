import { createSignal, For, onCleanup, onMount, Show } from "solid-js";
import { fillFor, layoutPx, tint } from "../lib/mosaic";
import { foldToBudget, frameLayout } from "../lib/view_budgets";
import { attentionOf, countsLine, totalOf, type CardModel, type SectionModel } from "../lib/explore_view";

// The mosaic (#840): area is what a card holds, colour is how much of it needs
// attention. Layout is the pure core's; this does the pixels.
//
// Three things here are deliberate and each fixes something an earlier render
// got wrong:
//
//   Tiles live INSIDE their frame, under a header that is reserved space
//   rather than an overlay. Absolutely positioned tiles appended after their
//   frame paint over its title, and z-order by DOM order is not a design.
//
//   The area budget runs BEFORE the layout, so a tile too small to click never
//   reaches the pixels and folds into a labelled remainder instead.
//
//   The frame is measured, not assumed. Pixel layout needs a real width, so a
//   ResizeObserver redraws when it changes.

const HEIGHT = 420;

type Tile = { card: CardModel; folded?: number };

export default function Mosaic(props: {
  sections: SectionModel[];
  onDrill: (id: string) => void;
  onHover: (h: { label: string; verdict: string } | null) => void;
}) {
  let host: HTMLDivElement | undefined;
  const [width, setWidth] = createSignal(0);

  onMount(() => {
    if (!host || typeof ResizeObserver === "undefined") {
      setWidth(host?.clientWidth || 960);
      return;
    }
    const ro = new ResizeObserver(() => setWidth(host?.clientWidth ?? 0));
    ro.observe(host);
    setWidth(host.clientWidth);
    onCleanup(() => ro.disconnect());
  });

  // jsdom reports no width, so the field would never draw in a test. Fall back
  // to a nominal frame rather than rendering nothing.
  const frameWidth = () => (width() > 0 ? width() : 960);

  return (
    <div ref={host} data-testid="explore-mosaic" class="relative w-full" style={{ height: `${HEIGHT}px` }}>
      <For each={layoutPx(props.sections.map((s) => ({ item: s, weight: Math.max(1, s.systems) })), frameWidth(), HEIGHT)}>
        {(frame) => (
          <SectionFrame
            section={frame.item}
            x={frame.x}
            y={frame.y}
            w={frame.w}
            h={frame.h}
            onDrill={props.onDrill}
            onHover={props.onHover}
          />
        )}
      </For>
    </div>
  );
}

function SectionFrame(props: {
  section: SectionModel;
  x: number;
  y: number;
  w: number;
  h: number;
  onDrill: (id: string) => void;
  onHover: (h: { label: string; verdict: string } | null) => void;
}) {
  // The z-order budget decides the header, not this component: a title is
  // reserved space and is given up before the contents are, and that rule
  // belongs in one place rather than once per renderer.
  const box = () => frameLayout(props.w, props.h);
  const headerH = () => box().headerH;
  const innerW = () => Math.max(1, box().w);
  const innerH = () => Math.max(1, box().h);

  // The area budget, before the layout rather than after it.
  const tiles = () => {
    const { drawn, folded } = foldToBudget(
      props.section.cards,
      (c) => Math.max(1, c.systems),
      innerW() * innerH(),
    );
    const out: Tile[] = drawn.map((card) => ({ card }));
    if (folded.length > 0) {
      const counts = folded.reduce(
        (acc, c) => ({
          healthy: acc.healthy + c.counts.healthy,
          incomplete: acc.incomplete + c.counts.incomplete,
          degraded: acc.degraded + c.counts.degraded,
          outage: acc.outage + c.counts.outage,
        }),
        { healthy: 0, incomplete: 0, degraded: 0, outage: 0 },
      );
      out.push({
        card: {
          id: `${props.section.id}-folded`,
          label: `+${folded.length} more`,
          type: props.section.cutType,
          systems: folded.reduce((n, c) => n + c.systems, 0),
          counts,
          field: { id: "folded", label: "", type: "", height: 0, items: [], children: [] },
        },
        folded: folded.length,
      });
    }
    return out;
  };

  return (
    <div
      class="absolute overflow-hidden rounded border border-base-content/30 bg-base-200"
      style={{ left: `${props.x}px`, top: `${props.y}px`, width: `${props.w}px`, height: `${props.h}px` }}
    >
      <Show when={headerH() > 0}>
        <div
          class="truncate px-1.5 font-semibold uppercase tracking-wider text-base-content/70"
          style={{ "font-size": "10px", "line-height": `${headerH()}px`, height: `${headerH()}px` }}
          title={props.section.label}
        >
          {props.section.label}
        </div>
      </Show>
      <div
        class="absolute"
        style={{
          left: `${box().x}px`,
          top: `${box().y}px`,
          width: `${innerW()}px`,
          height: `${innerH()}px`,
        }}
      >
        <For each={layoutPx(tiles().map((t) => ({ item: t, weight: Math.max(1, t.card.systems) })), innerW(), innerH())}>
          {(placed) => <MosaicTile tile={placed.item} x={placed.x} y={placed.y} w={placed.w} h={placed.h} onDrill={props.onDrill} onHover={props.onHover} />}
        </For>
      </div>
    </div>
  );
}

function MosaicTile(props: {
  tile: Tile;
  x: number;
  y: number;
  w: number;
  h: number;
  onDrill: (id: string) => void;
  onHover: (h: { label: string; verdict: string } | null) => void;
}) {
  const fill = () => (props.tile.folded ? { severity: "idle" as const, share: 0 } : fillFor(props.tile.card.counts));
  const describe = () =>
    props.onHover({
      label: props.tile.card.label,
      verdict: `${props.tile.card.type} · ${countsLine(props.tile.card.counts)}`,
    });
  return (
    <button
      type="button"
      data-tile={props.tile.card.id}
      class="absolute overflow-hidden text-left focus-visible:z-10 focus-visible:outline focus-visible:outline-2 focus-visible:outline-base-content"
      classList={{ "cursor-pointer": !props.tile.folded, "cursor-default": !!props.tile.folded }}
      style={{
        left: `${props.x}px`,
        top: `${props.y}px`,
        width: `${props.w}px`,
        height: `${props.h}px`,
        background: tint(fill()),
        "box-shadow": "inset 0 0 0 1px var(--color-base-200)",
      }}
      title={`${props.tile.card.label} · ${countsLine(props.tile.card.counts)}`}
      aria-label={`${props.tile.card.label}, ${totalOf(props.tile.card.counts)} systems, ${attentionOf(props.tile.card.counts)} needing attention`}
      onMouseEnter={describe}
      onFocus={describe}
      onClick={() => { if (!props.tile.folded) props.onDrill(props.tile.card.id); }}
    >
      {/* The label budget again, in pixels: a name is drawn only where it fits. */}
      <Show when={props.w >= 64 && props.h >= 18}>
        <span class="block truncate px-1 py-0.5 font-mono text-[9px] leading-tight text-base-100 mix-blend-luminosity">
          {props.tile.card.label}
        </span>
      </Show>
    </button>
  );
}
