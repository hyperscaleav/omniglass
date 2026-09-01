import { attentionOf, totalOf, type Counts } from "./explore_view";

// The mosaic's pure core (#840): where every tile sits, and what colour weight
// it carries. No DOM, because both are things an operator could dispute.
//
// Two rules here were learned by rendering the thing rather than by reasoning
// about it, and both are the point of this module existing:
//
//   1. Integer pixels, with adjacent edges rounded from the SAME float. A
//      layout in percentages leaves sub-pixel seams between tiles and, worse,
//      occasional overlaps, which read as data. Snapping each edge once and
//      deriving width from the snapped edges makes neighbours share an exact
//      boundary by construction rather than by luck.
//   2. Aggregate fill is a SHARE, not a rollup. Worst-wins is the right verdict
//      for one system and the wrong fill for a tile standing in for forty of
//      them: at any realistic failure rate almost every aggregate contains one
//      outage, so every tile saturates and the colour channel says nothing.

export type Weighted<T> = { item: T; weight: number };
export type Placed<T> = { item: T; x: number; y: number; w: number; h: number };

// worstRatio is the squarified treemap's cost function (Bruls, Huizing and van
// Wijk): the worst aspect ratio in a row, which the algorithm minimises to keep
// tiles near-square instead of long slivers nobody can click.
function worstRatio(row: number[], side: number): number {
  if (row.length === 0 || side <= 0) return Infinity;
  let sum = 0;
  let max = -Infinity;
  let min = Infinity;
  for (const v of row) {
    sum += v;
    if (v > max) max = v;
    if (v < min) min = v;
  }
  if (sum <= 0 || min <= 0) return Infinity;
  return Math.max((side * side * max) / (sum * sum), (sum * sum) / (side * side * min));
}

// squarify lays values out in a rectangle, in float space. Values are areas:
// the caller scales them so their sum is w * h.
function squarify<T>(items: Array<Weighted<T>>, x: number, y: number, w: number, h: number): Array<Placed<T>> {
  const out: Array<Placed<T>> = [];
  const queue = [...items];
  let guard = queue.length * 4 + 16;
  while (queue.length > 0 && guard-- > 0) {
    const side = Math.min(w, h);
    if (side <= 0) break;
    const row: Array<Weighted<T>> = [queue.shift() as Weighted<T>];
    while (queue.length > 0) {
      const widths = row.map((r) => r.weight);
      const trial = [...widths, queue[0].weight];
      if (worstRatio(trial, side) <= worstRatio(widths, side)) row.push(queue.shift() as Weighted<T>);
      else break;
    }
    const sum = row.reduce((a, r) => a + r.weight, 0);
    if (sum <= 0) continue;
    // Exactness by construction, not by correction. Float division leaves the
    // last row a fraction short of the frame, which snapping then turns into a
    // one or two pixel gap along an edge. So the last row consumes ALL the
    // remaining extent, and the last tile in every row runs exactly to that
    // row's far edge. Nothing is left over to round away.
    const last = queue.length === 0;
    if (w >= h) {
      const rw = last ? w : sum / h;
      let yy = y;
      row.forEach((r, i) => {
        const bottom = i === row.length - 1 ? y + h : yy + (r.weight / sum) * h;
        out.push({ item: r.item, x, y: yy, w: rw, h: bottom - yy });
        yy = bottom;
      });
      x += rw;
      w -= rw;
    } else {
      const rh = last ? h : sum / w;
      let xx = x;
      row.forEach((r, i) => {
        const right = i === row.length - 1 ? x + w : xx + (r.weight / sum) * w;
        out.push({ item: r.item, x: xx, y, w: right - xx, h: rh });
        xx = right;
      });
      y += rh;
      h -= rh;
    }
  }
  return out;
}

// layoutPx is the whole layout an renderer needs: weights scaled to the area,
// squarified, then snapped so every edge is an integer and neighbours share it.
//
// The snap is the part that matters. Rounding x and w independently lets two
// neighbours disagree about the boundary between them by a pixel, which is
// either a seam or an overlap. Rounding the two EDGES and deriving the width
// from them cannot: a tile's right edge and its neighbour's left edge are the
// same float, so they round to the same integer.
export function layoutPx<T>(items: Array<Weighted<T>>, w: number, h: number): Array<Placed<T>> {
  const usable = items.filter((i) => i.weight > 0);
  const total = usable.reduce((a, i) => a + i.weight, 0);
  if (total <= 0 || w < 1 || h < 1) return [];
  const area = w * h;
  const scaled = usable
    .map((i) => ({ item: i.item, weight: (i.weight / total) * area }))
    .sort((a, b) => b.weight - a.weight);
  // A tile whose float extent rounds to nothing is DROPPED, never widened to a
  // minimum. Widening it would steal a pixel from the neighbour it shares an
  // edge with, which is an overlap, and an overlap is exactly what the shared
  // edges exist to make impossible. Dropping is safe for the same reason: the
  // tile's two edges round to the same integer, so its neighbours are already
  // adjacent and no gap opens where it was.
  //
  // Keeping sub-pixel tiles out of here in the first place is the AREA budget's
  // job (view_budgets.foldToBudget). A caller that skips the fold loses the
  // smallest items rather than getting a broken frame, which is the right way
  // round.
  return squarify(scaled, 0, 0, w, h)
    .map((p) => {
      const x0 = Math.round(p.x);
      const y0 = Math.round(p.y);
      const x1 = Math.round(p.x + p.w);
      const y1 = Math.round(p.y + p.h);
      return { item: p.item, x: x0, y: y0, w: x1 - x0, h: y1 - y0 };
    })
    .filter((p) => p.w > 0 && p.h > 0);
}

export type Severity = "idle" | "healthy" | "incomplete" | "degraded" | "outage";

// A tile's colour, as a severity and how much of the tile is that way. The
// renderer turns this into pixels; keeping it as data is what makes "one
// outage in sixty must not look like twenty" a testable claim rather than a
// judgement about a screenshot.
export type Fill = { severity: Severity; share: number };

// The square root is deliberate. A linear share makes one bad room in sixty
// (1.7%) invisible, which is the opposite failure to worst-wins saturating
// everything: the point is that both ends of the range have to read.
export function fillFor(c: Counts): Fill {
  const total = totalOf(c);
  if (total === 0) return { severity: "idle", share: 0 };
  const bad = attentionOf(c);
  if (bad === 0) return { severity: "healthy", share: 0 };
  // Hue is the worst thing present, and incomplete is its own: a card whose
  // only trouble is unfinished commissioning must not read as degraded.
  const severity: Severity = c.outage > 0 ? "outage" : c.degraded > 0 ? "degraded" : "incomplete";
  return { severity, share: Math.sqrt(bad / total) };
}


// The tile's colour, as CSS. It lives beside fillFor because it is the other
// half of the same rule: the fill says how much and how bad, this says what
// that looks like, and both are testable without a DOM.
export function tint(fill: Fill): string {
  if (fill.severity === "idle") return "var(--color-base-300)";
  if (fill.severity === "healthy") return "var(--color-success)";
  const hue =
    fill.severity === "outage" ? "var(--color-error)"
    : fill.severity === "degraded" ? "var(--color-warning)"
    // The console's own commissioning hue (--og-incomplete, what
    // .badge-incomplete and .bg-incomplete paint with). Falling through to the
    // neutral here made an unfinished card look like an EMPTY one, which is the
    // one thing the mosaic must not confuse: a room with a gap in it has
    // something in it.
    : "var(--og-incomplete)";
  // A share of 0 would be pure green and a share of 1 pure red; in between the
  // tile reads as "mostly fine with something wrong" rather than as an alarm.
  const pct = Math.round(20 + fill.share * 80);
  return `color-mix(in srgb, ${hue} ${pct}%, var(--color-success))`;
}
