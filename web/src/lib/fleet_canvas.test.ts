import { describe, expect, it } from "vitest";
import {
  DOT_LAYOUT,
  FLAG_OWNED,
  FLAG_SHARED,
  VERDICT_UNKNOWN,
  canvasPalette,
  clusterAt,
  fillBuffer,
  hitTest,
  layoutBand,
  outlineFor,
  paintGroups,
  type CanvasPalette,
} from "./fleet_canvas";
import type { SystemCluster } from "./fleet";
import { verdictRank } from "./health";
import { uuidFor } from "./testids";

// The pure core of the fleet zoom: where every dot sits, what state it carries,
// and what colour a frame would paint it, all decided with no canvas and no
// DOM. jsdom has no layout and no 2d context, so width is a parameter and the
// paint plan is data; only Playwright ever proves pixels.

const cluster = (handle: string, dots: { c: string; v: string | null; owned?: boolean; shared?: boolean }[]): SystemCluster => ({
  systemId: uuidFor(handle),
  name: handle,
  label: handle,
  locationId: null,
  verdict: null,
  dots: dots.map((d, i) => ({
    componentId: uuidFor(d.c),
    name: `${d.c}-${i}`,
    verdict: d.v as SystemCluster["dots"][number]["verdict"],
    owned: d.owned ?? true,
    shared: d.shared ?? false,
  })),
});

const fakeRead = (tokens: Record<string, string>) => (t: string) => tokens[t] ?? "";

const palette = (): CanvasPalette =>
  canvasPalette(
    fakeRead({
      "--color-success": "#33cc88",
      "--color-warning": "#f0b232",
      "--color-error": "#f0676b",
      "--og-incomplete": "#8899aa",
      "--og-unknown": "#556677",
      "--og-outline": "#334455",
    }),
  );

describe("layoutBand", () => {
  it("within a cluster the pitch is dot+gap, and across clusters it is dot+gap+sysGap", () => {
    const l = layoutBand([cluster("ca", [{ c: "a1", v: "healthy" }, { c: "a2", v: "healthy" }]), cluster("cb", [{ c: "b1", v: "healthy" }])], 1000);
    const { dot, gap, sysGap } = l.opts;
    expect(l.xs[1] - l.xs[0]).toBe(dot + gap);
    expect(l.xs[2] - l.xs[1]).toBe(dot + gap + sysGap);
    expect(l.ys[0]).toBe(l.ys[2]);
  });

  it("places every dot and counts them all: nothing is silently dropped", () => {
    const clusters = [cluster("cc", Array.from({ length: 37 }, (_, i) => ({ c: `cc${i}`, v: "healthy" })))];
    const l = layoutBand(clusters, 100);
    expect(l.count).toBe(37);
    expect(l.componentIds).toHaveLength(37);
    for (let i = 0; i < l.count; i++) {
      expect(Number.isFinite(l.xs[i])).toBe(true);
      expect(l.componentIds[i]).toBeTruthy();
    }
  });

  it("wrapping stays inside the width", () => {
    const l = layoutBand([cluster("cw", Array.from({ length: 40 }, (_, i) => ({ c: `cw${i}`, v: "healthy" })))], 90);
    for (let i = 0; i < l.count; i++) {
      expect(l.xs[i] + l.opts.dot).toBeLessThanOrEqual(90);
    }
    expect(l.height).toBeGreaterThan(l.opts.dot);
  });

  it("a cluster that does not fit breaks to a new row rather than swallowing its gap", () => {
    // Width fits exactly three dots (3*7 + 2*2 = 25). The first cluster fills
    // the row; the second must start on a fresh row at x=0, not squeeze a
    // sysGap into space that cannot hold its first dot.
    const width = 3 * DOT_LAYOUT.dot + 2 * DOT_LAYOUT.gap + 2 * DOT_LAYOUT.padX;
    const l = layoutBand([cluster("cf", [{ c: "f1", v: "healthy" }, { c: "f2", v: "healthy" }, { c: "f3", v: "healthy" }]), cluster("cg", [{ c: "g1", v: "healthy" }])], width);
    // The break lands at the row start (padX), not squeezed after a gap.
    expect(l.xs[3]).toBe(DOT_LAYOUT.padX);
    expect(l.ys[3]).toBeGreaterThan(l.ys[2]);
  });

  it("returns per-cluster rects that cover their own dots", () => {
    const l = layoutBand([cluster("cr", [{ c: "r1", v: "healthy" }, { c: "r2", v: "healthy" }]), cluster("cs", [{ c: "s1", v: "healthy" }])], 1000);
    expect(l.clusters).toHaveLength(2);
    const r0 = l.clusters[0];
    expect(l.xs[0]).toBeGreaterThanOrEqual(r0.x);
    expect(l.xs[1] + l.opts.dot).toBeLessThanOrEqual(r0.x + r0.w);
    expect(l.clusterOf[0]).toBe(0);
    expect(l.clusterOf[2]).toBe(1);
  });
});

describe("hitTest", () => {
  it("a click at a dot's own left edge resolves to that dot, not its left neighbour", () => {
    const l = layoutBand([cluster("ch", [{ c: "h1", v: "healthy" }, { c: "h2", v: "healthy" }])], 1000);
    // One pixel inside dot 1's left edge sits within slop of dot 0's right
    // edge too; nearest centre must win.
    const px = l.xs[1] + 1;
    const py = l.ys[1] + 1;
    expect(hitTest(l, px, py)).toBe(1);
  });

  it("misses cleanly outside every dot", () => {
    const l = layoutBand([cluster("cm", [{ c: "m1", v: "healthy" }])], 1000);
    expect(hitTest(l, 500, 500)).toBe(-1);
  });

  it("clusterAt resolves a point to its cluster", () => {
    const l = layoutBand([cluster("cx", [{ c: "x1", v: "healthy" }]), cluster("cy", [{ c: "y1", v: "healthy" }])], 1000);
    expect(clusterAt(l, l.xs[0] + 1, l.ys[0] + 1)).toBe(0);
    expect(clusterAt(l, l.xs[1] + 1, l.ys[1] + 1)).toBe(1);
    expect(clusterAt(l, 900, 900)).toBe(-1);
  });
});

describe("fillBuffer", () => {
  it("carries per-dot verdict rank and ownership flags, and per-cluster the system's own verdict rank", () => {
    const clusters = [
      { ...cluster("cb1", [
        { c: "d-ok", v: "healthy", owned: true, shared: true },
        { c: "d-bad", v: "outage" },
      ]), verdict: "degraded" as const },
      { ...cluster("cb2", [{ c: "d-ghost", v: "healthy", owned: false, shared: true }]), verdict: "healthy" as const },
    ];
    const l = layoutBand(clusters, 1000);
    const buf = fillBuffer(l, clusters);
    expect(buf.verdict[0]).toBe(verdictRank("healthy"));
    expect(buf.verdict[1]).toBe(verdictRank("outage"));
    expect(buf.flags[0]).toBe(FLAG_OWNED | FLAG_SHARED);
    expect(buf.flags[2]).toBe(FLAG_SHARED);
    // The outline's fact: the SYSTEM verdict per cluster, independent of the
    // dots inside it (cb1's dots are healthy and outage; the system says
    // degraded, and the outline says what the system says).
    expect(buf.systemVerdict[0]).toBe(verdictRank("degraded"));
    expect(buf.systemVerdict[1]).toBe(verdictRank("healthy"));
  });

  it("an unrecognised verdict becomes the unknown sentinel, never a substituted word", () => {
    const clusters = [cluster("cu", [{ c: "d-null", v: null }])];
    const buf = fillBuffer(layoutBand(clusters, 1000), clusters);
    expect(buf.verdict[0]).toBe(VERDICT_UNKNOWN);
  });
});

describe("paintGroups", () => {
  it("a dot's fill is its own component verdict and nothing else: one channel, one meaning", () => {
    const clusters = [cluster("cq", [{ c: "q0", v: "healthy" }, { c: "q1", v: "outage" }, { c: "q2", v: "degraded" }, { c: "q3", v: "incomplete" }])];
    const buf = fillBuffer(layoutBand(clusters, 1000), clusters);
    const fills = paintGroups(buf, 4, palette()).map((g) => g.fill);
    expect(fills).toContain("#33cc88");
    expect(fills).toContain("#f0676b");
    expect(fills).toContain("#f0b232");
    expect(fills).toContain("#8899aa");
    // No identity hue anywhere: two systems with the same dot verdicts paint
    // the same colours, so a colour can never be misread as a status.
    for (const f of fills) expect(f).not.toContain("oklch");
  });

  it("the cluster outline wears the system's verdict, neutral when healthy", () => {
    const p = palette();
    expect(outlineFor(0, p)).toBe("#334455");
    expect(outlineFor(1, p)).toBe("#8899aa");
    expect(outlineFor(2, p)).toBe("#f0b232");
    expect(outlineFor(3, p)).toBe("#f0676b");
    expect(outlineFor(255, p)).toBe("#334455");
  });

  it("an owned dot and its ghost land in different groups, and the ghost group is stroke-only", () => {
    const clusters = [
      cluster("cs1", [{ c: "shared-box", v: "healthy", owned: true, shared: true }]),
      cluster("cs2", [{ c: "shared-box", v: "healthy", owned: false, shared: true }]),
    ];
    const buf = fillBuffer(layoutBand(clusters, 1000), clusters);
    const groups = paintGroups(buf, 2, palette());
    const ghost = groups.filter((g) => g.ghost);
    const solid = groups.filter((g) => !g.ghost);
    expect(ghost).toHaveLength(1);
    expect(solid).toHaveLength(1);
    expect(Array.from(ghost[0].indices)).toEqual([1]);
    expect(Array.from(solid[0].indices)).toEqual([0]);
  });

  it("grouping is by colour: a frame costs one fill per distinct colour, not one per dot", () => {
    const clusters = [cluster("cg1", Array.from({ length: 50 }, (_, i) => ({ c: `g${i}`, v: "healthy" })))];
    const buf = fillBuffer(layoutBand(clusters, 1000), clusters);
    const groups = paintGroups(buf, 50, palette());
    expect(groups).toHaveLength(1);
    expect(groups[0].indices).toHaveLength(50);
  });

  it("an unknown verdict lands in the unknown group", () => {
    const clusters = [cluster("cn", [{ c: "n1", v: null }])];
    const buf = fillBuffer(layoutBand(clusters, 1000), clusters);
    const groups = paintGroups(buf, 1, palette());
    expect(groups[0].fill).toBe("#556677");
  });
});

describe("canvasPalette", () => {
  it("resolves a distinct palette per theme reader", () => {
    const dark = canvasPalette(fakeRead({ "--color-error": "#f0676b", "--color-success": "#3ecf8e" }));
    const light = canvasPalette(fakeRead({ "--color-error": "#d6494d", "--color-success": "#16a06b" }));
    expect(dark.outage).toBe("#f0676b");
    expect(light.outage).toBe("#d6494d");
    expect(dark.healthy).toBe("#3ecf8e");
    expect(light.healthy).toBe("#16a06b");
  });

  it("falls back to a usable colour when the token read is empty (jsdom)", () => {
    const p = canvasPalette(fakeRead({}));
    for (const c of [p.healthy, p.outage, p.degraded, p.incomplete, p.unknown, p.outline.neutral]) expect(c).toBeTruthy();
  });
});
