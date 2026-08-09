import { describe, it, expect } from "vitest";
import { render } from "@solidjs/testing-library";
import HealthHistory from "./HealthHistory";
import type { HealthTransition } from "../lib/health";

// The history strip is the surface that answers "when did this change", so the
// one thing it must never do is render a state it knows as a state it does not.
// Both of its lookups (the segment tone and the row pill) fall back to the
// no-data styling for an unrecognized verdict, which makes every verdict the
// maps forget indistinguishable from never having been measured.
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

const at = (ts: string, verdict: string): HealthTransition =>
  ({ ts, verdict }) as HealthTransition;

describe("HealthHistory", () => {
  // #631 made incomplete a recordable verdict, so this component started
  // receiving a value neither of its maps knew. A commissioning gap is the
  // verdict a half-built estate sits in for months; painting it in the
  // no-data grey would tell an operator their history is missing when it is
  // in fact complete and says something specific.
  it("styles an incomplete stretch as its own state, not as no data", () => {
    const { getByText, container } = render(() => (
      <HealthHistory transitions={[at(ago(3 * 3600_000), "incomplete")]} verdict="incomplete" />
    ));

    const pill = getByText("incomplete");
    expect(pill.className).toContain("badge-incomplete");
    expect(pill.className).not.toContain("badge-ghost");

    // The strip's own empty track is legitimately bg-base-300, so this asserts on
    // the SEGMENT: every drawn stretch must carry a tone of its own, and none may
    // fall through to the track colour.
    const segments = [...container.querySelectorAll<HTMLElement>("[style*='flex']")].filter((el) =>
      el.className.startsWith("bg-"),
    );
    expect(segments.length).toBeGreaterThan(0);
    for (const seg of segments) {
      expect(seg.className).toBe("bg-incomplete");
    }
  });

  // The three verdicts that already worked must keep working, so the fix above
  // cannot be a special case bolted past the maps.
  it("gives each verdict a distinct tone and pill", () => {
    const seen = new Set<string>();
    for (const v of ["healthy", "incomplete", "degraded", "outage"]) {
      const { getByText, unmount } = render(() => (
        <HealthHistory transitions={[at(ago(3600_000), v)]} verdict={v} />
      ));
      const cls = getByText(v).className;
      expect(cls).not.toContain("badge-ghost");
      seen.add(cls);
      unmount();
    }
    expect(seen.size).toBe(4);
  });
});
