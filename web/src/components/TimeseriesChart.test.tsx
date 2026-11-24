import { describe, it, expect, afterEach } from "vitest";
import { render, cleanup, fireEvent } from "@solidjs/testing-library";
import TimeseriesChart from "./TimeseriesChart";
import { CursorProvider } from "../lib/cursor";

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

  it("step mode draws a carried-in value from the left edge when the window holds no change (#1488)", () => {
    const { getByTestId, queryByText } = render(() => <TimeseriesChart mode="step" samples={[]} carriedIn={1} now={now} windowMs={24 * 3600_000} />);
    expect(queryByText("No samples in the window.")).toBeNull();
    const d = getByTestId("timeseries-chart").querySelector("path[stroke-width]")?.getAttribute("d") ?? "";
    const [m, e] = d.split(" ").map((c) => c.slice(1).split(",").map(Number));
    expect(m[0]).toBeCloseTo(44, 2);
    expect(e[0]).toBeCloseTo(640 - 16, 2);
    expect(e[1]).toBe(m[1]);
  });
});

describe("TimeseriesChart on a shared cursor (#2424)", () => {
  // Two series sampled at different moments over one six-hour window.
  const a = [
    { ts: "2026-08-20T12:00:00Z", value: 10 },
    { ts: "2026-08-20T14:00:00Z", value: 20 },
    { ts: "2026-08-20T16:00:00Z", value: 30 },
  ];
  const b = [
    { ts: "2026-08-20T12:10:00Z", value: 1 },
    { ts: "2026-08-20T14:10:00Z", value: 2 },
    { ts: "2026-08-20T16:10:00Z", value: 3 },
  ];
  const win = 6 * 3600_000;
  // jsdom lays nothing out: give each chart the rect a 640-wide svg has.
  const sized = (svg: Element) => {
    (svg as SVGSVGElement).getBoundingClientRect = () => ({ left: 0, top: 0, width: 640, height: 170, right: 640, bottom: 170, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect;
    return svg;
  };
  // x of 14:00 in a 640 frame with pads 44/16 over 11:00..17:00.
  const x1400 = 44 + (3 / 6) * (640 - 44 - 16);

  const grid = () =>
    render(() => (
      <CursorProvider>
        <TimeseriesChart samples={a} now={now} windowMs={win} />
        <TimeseriesChart samples={b} now={now} windowMs={win} />
      </CursorProvider>
    ));

  it("hovering one chart draws a crosshair and value at the same instant on every chart; leaving clears all", () => {
    const { getAllByTestId, queryAllByTestId } = grid();
    const [svgA] = getAllByTestId("timeseries-chart").map(sized);
    fireEvent.mouseMove(svgA, { clientX: x1400 });
    const hairs = getAllByTestId("chart-crosshair");
    expect(hairs).toHaveLength(2);
    // Each snaps to its own nearest sample: A's 14:00 reads 20, B's 14:10 reads 2.
    expect(hairs[0].textContent).toContain("20.0");
    expect(hairs[1].textContent).toContain("2.0");
    // The crosshair line stands at the shared instant on both.
    const lineX = (h: Element) => h.querySelector("[data-testid=cursor-line]")!.getAttribute("x1");
    expect(Number(lineX(hairs[0]))).toBeCloseTo(x1400, 3);
    expect(lineX(hairs[1])).toBe(lineX(hairs[0]));
    fireEvent.mouseLeave(svgA);
    expect(queryAllByTestId("chart-crosshair")).toHaveLength(0);
  });

  it("the keyboard moves the cursor: focus lands on the newest point, arrows step the focused chart's points", () => {
    const { getAllByTestId, queryAllByTestId } = grid();
    const [svgA] = getAllByTestId("timeseries-chart");
    expect(svgA.getAttribute("tabindex")).toBe("0");
    fireEvent.focus(svgA);
    expect(getAllByTestId("chart-crosshair")[0].textContent).toContain("30.0");
    fireEvent.keyDown(svgA, { key: "ArrowLeft" });
    expect(getAllByTestId("chart-crosshair")[0].textContent).toContain("20.0");
    expect(getAllByTestId("chart-crosshair")[1].textContent).toContain("2.0");
    fireEvent.keyDown(svgA, { key: "Home" });
    expect(getAllByTestId("chart-crosshair")[0].textContent).toContain("10.0");
    fireEvent.keyDown(svgA, { key: "Escape" });
    expect(queryAllByTestId("chart-crosshair")).toHaveLength(0);
  });

  it("moving focus from one chart to another keeps the pointer's instant rather than jumping to the newest point", () => {
    const { getAllByTestId } = grid();
    const [svgA, svgB] = getAllByTestId("timeseries-chart").map(sized);
    fireEvent.focus(svgA);
    fireEvent.mouseMove(svgB, { clientX: x1400 });
    fireEvent.blur(svgA, { relatedTarget: svgB });
    fireEvent.focus(svgB);
    expect(getAllByTestId("chart-crosshair")[0].textContent).toContain("20.0");
  });

  it("a chart with no point near the cursor draws the line but no value", () => {
    const buckets = [{ ts: "2026-08-20T16:00:00Z", count: 2, min: 1, max: 2, avg: 1.5, last: 2 }];
    const { getAllByTestId } = render(() => (
      <CursorProvider>
        <TimeseriesChart samples={a} now={now} windowMs={win} />
        <TimeseriesChart buckets={buckets} resolutionMs={600_000} now={now} windowMs={win} />
      </CursorProvider>
    ));
    const [svgA] = getAllByTestId("timeseries-chart").map(sized);
    fireEvent.mouseMove(svgA, { clientX: x1400 });
    const hairs = getAllByTestId("chart-crosshair");
    expect(hairs[1].querySelector("[data-testid=cursor-line]")).toBeTruthy();
    expect(hairs[1].querySelector("[data-testid=cursor-value]")).toBeNull();
  });
});
