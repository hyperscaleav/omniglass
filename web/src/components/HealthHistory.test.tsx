import { describe, it, expect } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import HealthHistory from "./HealthHistory";
import type { HealthTransition } from "../lib/health";
import { fmtTime } from "../lib/format";

// The history strip is the surface that answers "when did this change", so the
// one thing it must never do is render a state it knows as a state it does not.
// Both of its lookups (the segment tone and the row pill) fall back to the
// no-data styling for an unrecognized health, which makes every health the
// maps forget indistinguishable from never having been measured.
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

const at = (ts: string, health: string): HealthTransition =>
  ({ ts, health: health }) as HealthTransition;

describe("HealthHistory", () => {
  // #631 made incomplete a recordable health, so this component started
  // receiving a value neither of its maps knew. A commissioning gap is the
  // health a half-built fleet sits in for months; painting it in the
  // no-data grey would tell an operator their history is missing when it is
  // in fact complete and says something specific.
  it("styles an incomplete stretch as its own state, not as no data", () => {
    const { getByText, container } = render(() => (
      <HealthHistory transitions={[at(ago(3 * 3600_000), "incomplete")]} health="incomplete" />
    ));

    const pill = getByText("incomplete");
    expect(pill.className).toContain("badge-incomplete");
    expect(pill.className).not.toContain("badge-ghost");

    // The strip's own empty track is legitimately bg-base-300, so this asserts on
    // the SEGMENT: every drawn stretch must carry a tone of its own, and none may
    // fall through to the track colour.
    const segments = [...container.querySelectorAll<HTMLElement>("[style*='flex']")].filter((el) =>
      typeof el.className === "string" && el.className.startsWith("bg-"),
    );
    expect(segments.length).toBeGreaterThan(0);
    for (const seg of segments) {
      expect(seg.className).toBe("bg-incomplete");
    }
  });

  // The three health results that already worked must keep working, so the fix above
  // cannot be a special case bolted past the maps.
  it("gives each health a distinct tone and pill", () => {
    const seen = new Set<string>();
    for (const v of ["healthy", "incomplete", "degraded", "outage"]) {
      const { getByText, unmount } = render(() => (
        <HealthHistory transitions={[at(ago(3600_000), v)]} health={v} />
      ));
      const cls = getByText(v).className;
      expect(cls).not.toContain("badge-ghost");
      seen.add(cls);
      unmount();
    }
    expect(seen.size).toBe(4);
  });

  // Healthy reads green (#1067), in the strip as in the pill: a grey healthy
  // stretch read as the no-data grey beside it.
  it("draws healthy in its own green, in the pill and the strip", () => {
    const { getByText, container } = render(() => <HealthHistory transitions={[at(ago(3600_000), "healthy")]} health="healthy" />);
    expect(getByText("healthy").className).toContain("badge-healthy");
    expect(container.querySelector(".bg-healthy")).toBeTruthy();
    expect(container.innerHTML).not.toMatch(/bg-base-content\/25/);
  });

  // The tooltips-not-prose rule (#790): the model this section teaches rides
  // the label's (i), never the flow. A future edit that re-inlines the
  // pedagogy fails here.
  it("carries no inline pedagogy: the explainer is the label's tooltip", () => {
    const { container } = render(() => <HealthHistory transitions={[at(ago(3600_000), "healthy")]} health="healthy" />);
    expect(container.textContent).not.toContain("One entry per change");
    expect(screen.getByRole("button", { name: /More about History/ })).toBeTruthy();
  });

  it("compact draws the strip only: no span rows and never the no-change fallback", () => {
    const { container, queryByText } = render(() => (
      <HealthHistory compact transitions={[at(ago(3600_000), "healthy"), at(ago(60_000), "degraded")]} health="degraded" />
    ));
    expect(container.querySelector(".og-statestrip")).toBeTruthy();
    expect(queryByText(/No change recorded/)).toBeNull();
    expect(queryByText(/held/)).toBeNull();
  });
});

// #2723: a caller that owns the clock moves the bar's newest segment with it.
describe("HealthHistory with a caller's now", () => {
  it("draws the newest segment up to the now it is given", () => {
    const t0 = Date.parse("2026-08-20T17:00:00Z");
    const weights = (now: number) => {
      const { container, unmount } = render(() => (
        <HealthHistory compact now={now} health="healthy" transitions={[at(new Date(t0 - 60_000).toISOString(), "degraded"), at(new Date(t0).toISOString(), "healthy")]} />
      ));
      const flex = [...container.querySelectorAll<HTMLElement>("[data-segment]")].map((el) => parseFloat(el.style.flexGrow));
      unmount();
      return flex;
    };
    const early = weights(t0 + 5_000);
    const later = weights(t0 + 60_000);
    // Degraded held 60 s; healthy has held 5 s then 60 s, so its share grows.
    expect(early[1] / early[0]).toBeCloseTo(5 / 60, 1);
    expect(later[1] / later[0]).toBeCloseTo(1, 1);
  });
});

// #2741: the bar opens at the range's start, the carried-in health drawn
// before the first edge inside it, so the bar and Uptime describe one window.
describe("HealthHistory over a range", () => {
  const t0 = Date.parse("2026-08-20T17:00:00Z");
  const iso = (ms: number) => new Date(ms).toISOString();

  it("starts at the range's start with the carried-in health", () => {
    const from = t0 - 3600_000;
    const { container } = render(() => (
      <HealthHistory compact now={t0} from={from} carriedIn="healthy" health="outage" transitions={[at(iso(t0 - 30 * 60_000), "outage")]} />
    ));
    expect(container.querySelector("[data-testid='strip-start']")!.textContent).toBe(fmtTime(iso(from)));
    const segs = [...container.querySelectorAll<HTMLElement>("[data-segment]")];
    expect(segs.map((el) => el.getAttribute("data-segment")!.split(":")[0])).toEqual(["healthy", "outage"]);
    expect(parseFloat(segs[0].style.flexGrow) / parseFloat(segs[1].style.flexGrow)).toBeCloseTo(1, 1);
  });

  it("with nothing carried in, starts at the first edge, as before", () => {
    const { container } = render(() => (
      <HealthHistory compact now={t0} from={t0 - 3600_000} health="outage" transitions={[at(iso(t0 - 30 * 60_000), "outage")]} />
    ));
    expect(container.querySelector("[data-testid='strip-start']")!.textContent).toBe(fmtTime(iso(t0 - 30 * 60_000)));
  });
});
