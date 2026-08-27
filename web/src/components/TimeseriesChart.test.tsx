import { describe, it, expect, afterEach } from "vitest";
import { render, cleanup } from "@solidjs/testing-library";
import TimeseriesChart from "./TimeseriesChart";

// The chart's chrome contract (#795 review): a time axis so the plot says
// WHEN, and a crosshair that appears only under a pointer. The crosshair's
// snapping itself is nearestPoint's, tested in the pure core; jsdom has no
// layout, so the hover interaction proves out in the live browser.
afterEach(cleanup);

const now = Date.parse("2026-08-20T17:00:00Z");
const samples = [
  { ts: "2026-08-20T05:00:00Z", value: 21.8 },
  { ts: "2026-08-20T16:00:00Z", value: 23.5 },
];

describe("TimeseriesChart", () => {
  it("draws four time ticks along the bottom axis", () => {
    const { getByTestId } = render(() => <TimeseriesChart samples={samples} now={now} windowMs={24 * 3600_000} />);
    const svg = getByTestId("timeseries-chart");
    const ticks = [...svg.querySelectorAll("text")].filter((t) => /^\d{1,2}:\d{2}/.test(t.textContent ?? ""));
    expect(ticks.length).toBeGreaterThanOrEqual(4);
  });

  it("renders no crosshair until a pointer is over it", () => {
    const { getByTestId, queryByTestId } = render(() => <TimeseriesChart samples={samples} now={now} windowMs={24 * 3600_000} />);
    expect(getByTestId("timeseries-chart")).toBeTruthy();
    expect(queryByTestId("chart-crosshair")).toBeNull();
  });

  it("a sparkline stays chromeless: no axis, no hover machinery", () => {
    const { getByTestId } = render(() => <TimeseriesChart spark samples={samples} now={now} windowMs={24 * 3600_000} />);
    const svg = getByTestId("sparkline");
    expect(svg.querySelectorAll("text").length).toBe(0);
  });
});
