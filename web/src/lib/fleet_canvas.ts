// The fleet canvas pure core (#633): where every dot sits, what state it
// carries, and what colour a frame would paint it, all as pure functions of
// their inputs. The impure shell (BandCanvas) does six things in a paint
// function and nothing else; everything an operator could dispute is decided
// here, where it unit-tests with no canvas and no DOM.
//
// The layout ports from omniglass-lab's overview.js (layoutEstate), with the
// lab's own vocabulary re-expressed against the fleet wire and three of its
// bugs fixed rather than inherited (each pinned in fleet_canvas.test.ts):
//   - arrays were sized by the device list but filled only for placed devices,
//     so an unplaced device was silently dropped and `count` was the only
//     truth: here the arrays are sized by the true dot count and every dot is
//     placed;
//   - hitTest returned the first linear-scan match with a slop that
//     overlapped the next dot, so every dot but a row's first had a one-pixel
//     column resolving to its left neighbour: here a hit is the nearest
//     centre among candidates;
//   - sysGap was added before the wrap check, so a cluster boundary at the
//     right edge swallowed its gap: here a cluster that does not fit breaks
//     to a fresh row.
//
// The state buffer is createHeatModel's omniglass form: parallel typed arrays
// the paint loop reads, grouped so a frame costs one fillStyle per distinct
// colour regardless of dot count. The lab's transient lanes (heat, flash,
// decay) are deliberately NOT ported: no event channel reaches this console
// yet, and a decay loop with no producer is dead code driving an untested
// clock. When the live channel lands (follow-up under #630), the transient
// lane slots in beside `verdict` as more arrays in DotBuffer and a second
// paint pass; nothing here needs to move.

import type { SystemCluster } from "./fleet";
import { verdictRank } from "./health";

export type DotLayoutOpts = {
  dot: number;
  gap: number;
  sysGap: number;
  rowGap: number;
  padX: number;
  padY: number;
};

// The lab's proven measures, minus its label row and band padding: DOM owns
// the chrome here, so the canvas is only the dot field.
export const DOT_LAYOUT: DotLayoutOpts = { dot: 8, gap: 3, sysGap: 12, rowGap: 10, padX: 4, padY: 4 };

export type ClusterRect = {
  systemId: string;
  x: number;
  y: number;
  w: number;
  h: number;
  // from/to are the dot index range [from, to) this cluster owns.
  from: number;
  to: number;
};

export type DotLayout = {
  width: number;
  height: number;
  count: number;
  xs: Float32Array;
  ys: Float32Array;
  componentIds: string[];
  clusterOf: Uint16Array;
  clusters: ClusterRect[];
  opts: DotLayoutOpts;
};

// layoutBand places one band's clusters into a wrapping dot field of the given
// width. Width is a parameter and never a measurement: jsdom has no layout,
// and the whole point of the pure core is that the same inputs produce the
// same dots anywhere.
export function layoutBand(clusters: SystemCluster[], width: number, optsIn?: Partial<DotLayoutOpts>): DotLayout {
  const o: DotLayoutOpts = { ...DOT_LAYOUT, ...optsIn };
  const usable = Math.max(width - o.padX * 2, o.dot);

  const total = clusters.reduce((n, c) => n + c.dots.length, 0);
  const xs = new Float32Array(total);
  const ys = new Float32Array(total);
  const componentIds = new Array<string>(total);
  const clusterOf = new Uint16Array(total);
  const rects: ClusterRect[] = [];

  let i = 0;
  let x = 0; // relative to padX
  let rowY = o.padY;
  for (let ci = 0; ci < clusters.length; ci++) {
    const c = clusters[ci];
    if (c.dots.length === 0) {
      rects.push({ systemId: c.systemId, x: o.padX + x, y: rowY, w: 0, h: 0, from: i, to: i });
      continue;
    }
    // The gap before a new cluster, unless the cluster's first dot cannot fit
    // after it: then break to a fresh row instead of swallowing the gap at
    // the edge.
    if (x > 0) {
      if (x + o.sysGap + o.dot > usable) {
        x = 0;
        rowY += o.dot + o.rowGap;
      } else {
        x += o.sysGap;
      }
    }
    const from = i;
    let minX = Infinity;
    let maxX = -Infinity;
    const top = rowY;
    for (const d of c.dots) {
      if (x + o.dot > usable && x > 0) {
        x = 0;
        rowY += o.dot + o.rowGap;
      }
      xs[i] = o.padX + x;
      ys[i] = rowY;
      componentIds[i] = d.componentId;
      clusterOf[i] = ci;
      minX = Math.min(minX, xs[i]);
      maxX = Math.max(maxX, xs[i] + o.dot);
      i++;
      x += o.dot + o.gap;
    }
    // The trailing intra-cluster gap stays in the running x (the lab's
    // proven pitch: cross-cluster is dot+gap+sysGap); the cluster's own
    // footprint is tracked by minX/maxX, which never include it.
    rects.push({
      systemId: c.systemId,
      x: minX,
      y: top,
      w: maxX - minX,
      h: rowY + o.dot - top,
      from,
      to: i,
    });
  }

  return {
    width,
    height: total === 0 ? 0 : rowY + o.dot + o.padY,
    count: i,
    xs,
    ys,
    componentIds,
    clusterOf,
    clusters: rects,
    opts: o,
  };
}

// hitTest maps a canvas-space point to a dot index, or -1. Candidates within
// slop of a dot's box are ranked by distance to the dot's centre, so a point
// on the boundary between two dots resolves to the one it is actually on.
export function hitTest(layout: DotLayout, px: number, py: number, slop = 1): number {
  const d = layout.opts.dot;
  let best = -1;
  let bestDist = Infinity;
  for (let i = 0; i < layout.count; i++) {
    if (px < layout.xs[i] - slop || px > layout.xs[i] + d + slop) continue;
    if (py < layout.ys[i] - slop || py > layout.ys[i] + d + slop) continue;
    const cx = layout.xs[i] + d / 2;
    const cy = layout.ys[i] + d / 2;
    const dist = (px - cx) * (px - cx) + (py - cy) * (py - cy);
    if (dist < bestDist) {
      bestDist = dist;
      best = i;
    }
  }
  return best;
}

// clusterAt resolves a point to a cluster index via the cluster rects, or -1.
// A rect spans wrapped rows, so this is a bounding-box answer: precise enough
// for hover chrome, and a dot-level answer is hitTest's job.
export function clusterAt(layout: DotLayout, px: number, py: number): number {
  for (let ci = 0; ci < layout.clusters.length; ci++) {
    const r = layout.clusters[ci];
    if (r.to > r.from && px >= r.x && px <= r.x + r.w && py >= r.y && py <= r.y + r.h) return ci;
  }
  return -1;
}

// The per-dot state the paint loop reads, in parallel typed arrays rather
// than objects: at fleet scale the buffer is the point.
export type DotBuffer = {
  verdict: Uint8Array;
  flags: Uint8Array;
  // systemVerdict is per CLUSTER (indexed by cluster, not dot): the rank the
  // outline wears.
  systemVerdict: Uint8Array;
};

export const VERDICT_UNKNOWN = 255;
export const FLAG_OWNED = 1;
export const FLAG_SHARED = 2;

export function fillBuffer(layout: DotLayout, clusters: SystemCluster[]): DotBuffer {
  const verdict = new Uint8Array(layout.count);
  const flags = new Uint8Array(layout.count);
  const systemVerdict = new Uint8Array(clusters.length);
  let i = 0;
  clusters.forEach((c, ci) => {
    systemVerdict[ci] = c.verdict === null ? VERDICT_UNKNOWN : verdictRank(c.verdict);
    for (const d of c.dots) {
      verdict[i] = d.verdict === null ? VERDICT_UNKNOWN : verdictRank(d.verdict);
      flags[i] = (d.owned ? FLAG_OWNED : 0) | (d.shared ? FLAG_SHARED : 0);
      i++;
    }
  });
  return { verdict, flags, systemVerdict };
}

// The CSS custom-property boundary. ctx.fillStyle cannot read var(), and
// getComputedStyle returns "" under jsdom, so the reader is a parameter with
// a documented fallback per token: the fallbacks are the dark theme's own
// values, so a canvas painted before the tokens resolve is wrong-theme, not
// invisible.
export type CanvasPalette = {
  healthy: string;
  incomplete: string;
  degraded: string;
  outage: string;
  unknown: string;
  ghostAlpha: number;
  // The cluster outline colours, one per system verdict, plus the neutral
  // one a healthy or unknown system wears.
  outline: { neutral: string; incomplete: string; degraded: string; outage: string };
};

export function canvasPalette(read: (token: string) => string): CanvasPalette {
  const token = (name: string, fallback: string) => {
    const v = read(name).trim();
    return v === "" ? fallback : v;
  };
  const incomplete = token("--og-incomplete", "#8494ab");
  const degraded = token("--color-warning", "#f0b232");
  const outage = token("--color-error", "#f0676b");
  return {
    // A dot's colour is its component's verdict and nothing else: one channel,
    // one meaning. The system's verdict rides the CLUSTER OUTLINE (below).
    healthy: token("--color-success", "#3ecf8e"),
    incomplete,
    degraded,
    outage,
    unknown: token("--og-unknown", "#64748b"),
    ghostAlpha: 0.45,
    outline: {
      neutral: token("--og-outline", "rgba(148,163,184,0.25)"),
      incomplete,
      degraded,
      outage,
    },
  };
}

// One paint group is one fillStyle write: the whole "how few state changes
// does a frame need" question answered as data. Group key is ghost-or-solid
// plus the colour, and the colour is the dot's own verdict: healthy, incomplete,
// degraded, outage, or unknown. Nothing else colours a dot (the identity-hue
// scheme ADR-0124 tried was reversed for operator clarity: one channel, one
// meaning; the system's verdict is the cluster outline's job).
export type PaintGroup = {
  fill: string;
  ghost: boolean;
  indices: Uint32Array;
};

// Verdict ranks, mirrored from health.ts's RANK: 0 healthy, 1 incomplete,
// 2 degraded, 3 outage; VERDICT_UNKNOWN for anything the console cannot narrow.
export function paintGroups(buf: DotBuffer, count: number, palette: CanvasPalette): PaintGroup[] {
  const byKey = new Map<string, { fill: string; ghost: boolean; indices: number[] }>();
  for (let i = 0; i < count; i++) {
    const ghost = (buf.flags[i] & FLAG_SHARED) !== 0 && (buf.flags[i] & FLAG_OWNED) === 0;
    let fill: string;
    switch (buf.verdict[i]) {
      case 0:
        fill = palette.healthy;
        break;
      case 1:
        fill = palette.incomplete;
        break;
      case 2:
        fill = palette.degraded;
        break;
      case 3:
        fill = palette.outage;
        break;
      default:
        fill = palette.unknown;
    }
    const key = `${ghost ? "g" : "s"}:${fill}`;
    const group = byKey.get(key);
    if (group) group.indices.push(i);
    else byKey.set(key, { fill, ghost, indices: [i] });
  }
  return [...byKey.values()].map((g) => ({ fill: g.fill, ghost: g.ghost, indices: Uint32Array.from(g.indices) }));
}

// outlineFor picks the cluster outline colour for a system verdict rank (the
// same 0..3 the buffer uses), neutral for healthy or unknown.
export function outlineFor(rank: number, palette: CanvasPalette): string {
  switch (rank) {
    case 1:
      return palette.outline.incomplete;
    case 2:
      return palette.outline.degraded;
    case 3:
      return palette.outline.outage;
    default:
      return palette.outline.neutral;
  }
}
