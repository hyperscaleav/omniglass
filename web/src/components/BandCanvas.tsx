import { createEffect, createMemo, createSignal, onCleanup, onMount } from "solid-js";
import { fillBuffer, hitTest, layoutBand, outlineFor, paintGroups, canvasPalette, FLAG_OWNED, FLAG_SHARED, type DotLayout } from "../lib/fleet_canvas";
import type { Dot, SystemCluster } from "../lib/fleet";

// One band's dot field, on canvas (#633). This is the impure shell around the
// pure core: everything an operator could dispute (where a dot sits, what
// colour it wears, what a click means) is decided in fleet_canvas.ts and
// unit-tested there; this component does I/O in one paint function and wiring.
//
// Solid notes, because the habits that break here are React's:
//   - props are never destructured (a destructure freezes the first value and
//     the canvas would paint the first fleet forever);
//   - createEffect subscribes to what it READS, so layout() and buffer() are
//     read inside the effect;
//   - cleanup is onCleanup, not a returned function (a returned teardown is
//     silently ignored and the ResizeObserver would observe forever);
//   - the ref is a plain variable: Solid's JSX is eager DOM and el is the real
//     element synchronously, no .current, no null-first-render.
export default function BandCanvas(props: {
  clusters: SystemCluster[];
  ariaLabel: string;
  // square: component dots inside a system outline (the location zoom).
  // round: one system per cluster, the dot IS the system, no outline (the
  // fleet zoom, design option B: the shape is the grain).
  shape?: "square" | "round";
  onHoverDot?: (dot: Dot | null) => void;
  onClickDot?: (dot: Dot) => void;
}) {
  let el!: HTMLCanvasElement;
  let wrap!: HTMLDivElement;
  const [width, setWidth] = createSignal(0);

  const layout = createMemo<DotLayout>(() =>
    layoutBand(props.clusters, Math.max(width(), 0), props.shape === "round" ? { dot: 11, gap: 5, sysGap: 5, rowGap: 7, padX: 0, padY: 3 } : undefined),
  );
  const buffer = createMemo(() => fillBuffer(layout(), props.clusters));

  // The dot a canvas point resolves to, for hover chrome. Flat list mirroring
  // the layout's dot order, which is cluster order then dot order.
  const flatDots = createMemo<Dot[]>(() => props.clusters.flatMap((c) => c.dots));

  let raf = 0;
  const schedule = () => {
    if (raf) return;
    raf = requestAnimationFrame(() => {
      raf = 0;
      paint();
    });
  };

  // The whole impure surface, in order: read the theme tokens, size the
  // element and its backing store from the SAME dpr sample, clear, one
  // fillRect pass per paint group, one stroke pass for rings and ghosts.
  // Sampling dpr once per paint (element size and transform together) is the
  // fix for the lab's divergence bug, where a drag to a different-dpr monitor
  // clipped the canvas until the next rebuild.
  function paint() {
    const ctx = el.getContext("2d");
    if (!ctx) return; // jsdom: the chrome renders, the pixels are Playwright's job
    const l = layout();
    const buf = buffer();
    const dpr = window.devicePixelRatio || 1;
    el.style.width = `${l.width}px`;
    el.style.height = `${l.height}px`;
    el.width = Math.max(1, Math.round(l.width * dpr));
    el.height = Math.max(1, Math.round(l.height * dpr));
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, l.width, l.height);

    // Repaints are rare (data, resize, theme), so resolving the palette per
    // paint is cheaper than watching the theme and can never go stale.
    const palette = canvasPalette((t) => getComputedStyle(document.documentElement).getPropertyValue(t));
    const d = l.opts.dot;
    const round = props.shape === "round";
    // Pass 0 (square only): the cluster outline, one rounded rect per system,
    // in the colour of the SYSTEM's verdict (neutral when healthy). Two facts,
    // two channels: the outline says how the room reads, the dots inside say
    // which boxes. A round mark is the system itself, so it takes no outline.
    const pad = 3;
    ctx.lineWidth = 1;
    for (let ci = 0; !round && ci < l.clusters.length; ci++) {
      const r = l.clusters[ci];
      if (r.to <= r.from) continue;
      ctx.strokeStyle = outlineFor(buf.systemVerdict[ci], palette);
      const x = r.x - pad + 0.5;
      const y = r.y - pad + 0.5;
      const w = r.w + pad * 2 - 1;
      const h = r.h + pad * 2 - 1;
      const rad = 3;
      ctx.beginPath();
      ctx.moveTo(x + rad, y);
      ctx.arcTo(x + w, y, x + w, y + h, rad);
      ctx.arcTo(x + w, y + h, x, y + h, rad);
      ctx.arcTo(x, y + h, x, y, rad);
      ctx.arcTo(x, y, x + w, y, rad);
      ctx.closePath();
      ctx.stroke();
    }
    for (const group of paintGroups(buf, l.count, palette)) {
      if (group.ghost) {
        // A ghost is stroke-only: the box exists, it lives elsewhere.
        ctx.globalAlpha = palette.ghostAlpha;
        ctx.strokeStyle = group.fill;
        ctx.lineWidth = 1;
        for (const i of group.indices) ctx.strokeRect(l.xs[i] + 0.5, l.ys[i] + 0.5, d - 1, d - 1);
        ctx.globalAlpha = 1;
      } else if (round) {
        ctx.fillStyle = group.fill;
        const r = d / 2;
        for (const i of group.indices) {
          ctx.beginPath();
          ctx.arc(l.xs[i] + r, l.ys[i] + r, r, 0, Math.PI * 2);
          ctx.fill();
        }
      } else {
        ctx.fillStyle = group.fill;
        for (const i of group.indices) ctx.fillRect(l.xs[i], l.ys[i], d, d);
      }
    }
    // The ring pass: a shared component drawn solid in its primary cluster
    // wears a ring so the ghost elsewhere has a visible owner.
    ctx.strokeStyle = palette.unknown;
    ctx.lineWidth = 1;
    for (let i = 0; i < l.count; i++) {
      if ((buf.flags[i] & FLAG_OWNED) !== 0 && (buf.flags[i] & FLAG_SHARED) !== 0) {
        ctx.strokeRect(l.xs[i] - 1.5, l.ys[i] - 1.5, d + 3, d + 3);
      }
    }
  }

  onMount(() => {
    setWidth(wrap.clientWidth);
    // jsdom has no ResizeObserver and zero layout, so guard it; the width
    // signal then stays 0 and the layout still computes (usable clamps to one
    // dot), which is what lets the chrome tests mount this component.
    if (typeof ResizeObserver !== "undefined") {
      let timer: ReturnType<typeof setTimeout> | undefined;
      const ro = new ResizeObserver(() => {
        clearTimeout(timer);
        timer = setTimeout(() => {
          const w = wrap.clientWidth;
          if (w !== width()) setWidth(w);
        }, 120);
      });
      ro.observe(wrap);
      onCleanup(() => {
        clearTimeout(timer);
        ro.disconnect();
      });
    }
  });

  createEffect(() => {
    layout();
    buffer();
    schedule();
  });
  onCleanup(() => cancelAnimationFrame(raf));

  function onClick(e: MouseEvent) {
    const rect = el.getBoundingClientRect();
    const i = hitTest(layout(), e.clientX - rect.x, e.clientY - rect.y);
    const dot = i >= 0 ? (flatDots()[i] ?? null) : null;
    if (dot) props.onClickDot?.(dot);
  }

  function onMove(e: MouseEvent) {
    const rect = el.getBoundingClientRect();
    const i = hitTest(layout(), e.clientX - rect.x, e.clientY - rect.y);
    const dot = i >= 0 ? (flatDots()[i] ?? null) : null;
    // The native tooltip: zero positioning code, and assertable by reading
    // the attribute.
    el.title = dot ? `${dot.name}${dot.verdict ? ` (${dot.verdict})` : ""}` : "";
    props.onHoverDot?.(dot);
  }

  return (
    <div ref={wrap} class="min-w-0 flex-1">
      <canvas
        ref={el}
        role="img"
        aria-label={props.ariaLabel}
        onMouseMove={onMove}
        onMouseLeave={() => props.onHoverDot?.(null)}
        onClick={onClick}
        classList={{ "cursor-pointer": !!props.onClickDot }}
      />
    </div>
  );
}
