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

describe("TimeseriesChart over buckets and steps (#1269)", () => {
  const buckets = [
    { ts: "2026-08-20T14:00:00Z", count: 4, min: 20, max: 30, avg: 25, last: 26 },
    { ts: "2026-08-20T15:00:00Z", count: 0, min: null, max: null, avg: null, last: null },
    { ts: "2026-08-20T16:00:00Z", count: 2, min: 22, max: 24, avg: 23, last: 24 },
  ];

  it("draws the min/max band under the avg line, and the newest bucket's last as the end label", () => {
    const { getByTestId } = render(() => <TimeseriesChart buckets={buckets} resolutionMs={3600_000} now={now} windowMs={6 * 3600_000} />);
    const svg = getByTestId("timeseries-chart");
    expect(getByTestId("chart-band").getAttribute("d")).toBeTruthy();
    expect(svg.textContent).toContain("24.0");
  });

  it("an all-empty bucketed read says there are no samples, not an empty plot", () => {
    const empty = buckets.map((b) => ({ ...b, count: 0, min: null, max: null, avg: null, last: null }));
    const { queryByTestId, getByText } = render(() => <TimeseriesChart buckets={empty} resolutionMs={3600_000} now={now} windowMs={6 * 3600_000} />);
    expect(queryByTestId("timeseries-chart")).toBeNull();
    expect(getByText("No samples in the window.")).toBeTruthy();
  });

  it("raw samples draw no band", () => {
    const { queryByTestId } = render(() => <TimeseriesChart samples={samples} now={now} windowMs={24 * 3600_000} />);
    expect(queryByTestId("chart-band")).toBeNull();
  });

  it("step mode draws a property as steps: every move is a vertical edge", () => {
    const { getByTestId } = render(() => <TimeseriesChart mode="step" samples={samples} now={now} windowMs={24 * 3600_000} />);
    const d = getByTestId("timeseries-chart").querySelector("path[stroke-width]")?.getAttribute("d") ?? "";
    const m = d.match(/L([\d.]+),([\d.]+) L([\d.]+),([\d.]+)/);
    expect(m).toBeTruthy();
    expect(m![1]).toBe(m![3]);
  });
});
