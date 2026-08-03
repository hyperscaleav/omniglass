import { describe, expect, it } from "vitest";
import { createSignal } from "solid-js";
import { render } from "@solidjs/testing-library";
import type { ViewResult } from "../../lib/views";
import LineSeries from "./LineSeries";
import StatTiles from "./StatTiles";
import StatusGrid from "./StatusGrid";
import ViewTable from "./ViewTable";

const reachability = (states: string[]): ViewResult => ({
  columns: [
    { name: "component", type: "string", role: "label" },
    { name: "interface", type: "string" },
    { name: "state", type: "string", role: "value" },
    { name: "since", type: "time", role: "time" },
  ],
  rows: states.map((s, i) => [`comp-${i}`, `if-${i}`, s, "2026-08-03T12:00:00Z"]),
});

const reachabilityMap = { label: "component", value: "state", time: "since" };

const counts: ViewResult = {
  columns: [
    { name: "tile", type: "string", role: "label" },
    { name: "count", type: "number", role: "value" },
  ],
  rows: [
    ["components", 12],
    ["interfaces", 30],
  ],
};
const countsMap = { label: "tile", value: "count" };

const series: ViewResult = {
  columns: [
    { name: "ts", type: "time", role: "time" },
    { name: "instance", type: "string", role: "series" },
    { name: "value", type: "number", role: "value" },
  ],
  rows: [
    ["2026-08-03T12:00:00Z", "eth0", 3],
    ["2026-08-03T12:01:00Z", "eth0", 5],
    ["2026-08-03T12:02:00Z", "eth0", 4],
  ],
};
const seriesMap = { time: "ts", series: "instance", value: "value" };

describe("StatTiles", () => {
  it("renders one tile per row with its label and value", () => {
    const { getByText } = render(() => <StatTiles result={() => counts} mapping={countsMap} />);
    expect(getByText("components")).toBeTruthy();
    expect(getByText("12")).toBeTruthy();
    expect(getByText("interfaces")).toBeTruthy();
    expect(getByText("30")).toBeTruthy();
  });

  it("renders an empty state rather than a bare frame", () => {
    const { getByText } = render(() => (
      <StatTiles result={() => ({ ...counts, rows: [] })} mapping={countsMap} />
    ));
    expect(getByText(/nothing to show/i)).toBeTruthy();
  });
});

describe("ViewTable", () => {
  it("renders a header per column and a row per result row", () => {
    const { container } = render(() => <ViewTable result={() => reachability(["up", "down"])} />);
    expect(container.querySelectorAll("thead th").length).toBe(4);
    expect(container.querySelectorAll("tbody tr").length).toBe(2);
    expect(container.querySelectorAll("tbody tr")[1].textContent).toContain("down");
  });

  it("renders an empty state when the result carries no rows", () => {
    const { getByText, container } = render(() => (
      <ViewTable result={() => ({ ...reachability([]), rows: [] })} />
    ));
    expect(container.querySelectorAll("tbody tr").length).toBe(0);
    expect(getByText(/nothing to show/i)).toBeTruthy();
  });

  it("renders nothing but a placeholder before the first result arrives", () => {
    const { getByText } = render(() => <ViewTable result={() => undefined} />);
    expect(getByText(/loading/i)).toBeTruthy();
  });

  // The fine-grained-update pin: this is why the renderers apply results
  // through a store with reconcile. A one-row delta must patch that row's
  // cells, leaving every sibling row's DOM node untouched. Re-assigning the
  // rows array instead would recreate the whole tbody, which is invisible to a
  // content assertion and expensive on a live surface.
  it("updates one row's cells in place, leaving sibling row nodes identical", async () => {
    const [result, setResult] = createSignal<ViewResult>(reachability(["up", "up", "up"]));
    const { container } = render(() => <ViewTable result={result} />);
    const before = [...container.querySelectorAll("tbody tr")];
    expect(before.length).toBe(3);

    setResult(reachability(["up", "down", "up"]));
    const after = [...container.querySelectorAll("tbody tr")];
    expect(after.length).toBe(3);
    // The unchanged rows are the SAME DOM nodes, not equal-looking new ones.
    expect(after[0]).toBe(before[0]);
    expect(after[2]).toBe(before[2]);
    // The changed row kept its node too: only the cell's text was patched.
    expect(after[1]).toBe(before[1]);
    expect(after[1].textContent).toContain("down");
  });
});

describe("StatusGrid", () => {
  it("renders a keyed cell per row carrying the value as its status", () => {
    const { container } = render(() => (
      <StatusGrid result={() => reachability(["up", "down", "unknown"])} mapping={reachabilityMap} />
    ));
    const cells = container.querySelectorAll("[data-state]");
    expect(cells.length).toBe(3);
    expect([...cells].map((c) => c.getAttribute("data-state"))).toEqual(["up", "down", "unknown"]);
  });

  it("labels each cell with the mapped label column", () => {
    const { getByText } = render(() => (
      <StatusGrid result={() => reachability(["up"])} mapping={reachabilityMap} />
    ));
    expect(getByText("comp-0")).toBeTruthy();
  });

  it("fails loudly when the field-mapping names a column the result lost", () => {
    expect(() =>
      render(() => (
        <StatusGrid result={() => reachability(["up"])} mapping={{ ...reachabilityMap, value: "verdict" }} />
      )),
    ).toThrowError(/verdict/);
  });

  it("renders an empty state", () => {
    const { getByText } = render(() => (
      <StatusGrid result={() => ({ ...reachability([]), rows: [] })} mapping={reachabilityMap} />
    ));
    expect(getByText(/nothing to show/i)).toBeTruthy();
  });
});

describe("LineSeries", () => {
  it("plots one polyline point per row, scaled into the viewport", () => {
    const { container } = render(() => <LineSeries result={() => series} mapping={seriesMap} />);
    const poly = container.querySelector("polyline");
    expect(poly).toBeTruthy();
    expect(poly!.getAttribute("points")!.trim().split(/\s+/).length).toBe(3);
  });

  it("draws one polyline per series instance", () => {
    const twoSeries: ViewResult = {
      ...series,
      rows: [...series.rows, ["2026-08-03T12:00:00Z", "eth1", 9], ["2026-08-03T12:01:00Z", "eth1", 10]],
    };
    const { container } = render(() => <LineSeries result={() => twoSeries} mapping={seriesMap} />);
    expect(container.querySelectorAll("polyline").length).toBe(2);
  });

  it("skips rows with no numeric value, so a categorical lane does not plot as zero", () => {
    const mixed: ViewResult = {
      ...series,
      rows: [
        ["2026-08-03T12:00:00Z", "eth0", 3],
        ["2026-08-03T12:01:00Z", "eth0", null],
        ["2026-08-03T12:02:00Z", "eth0", 4],
      ],
    };
    const { container } = render(() => <LineSeries result={() => mixed} mapping={seriesMap} />);
    expect(container.querySelector("polyline")!.getAttribute("points")!.trim().split(/\s+/).length).toBe(2);
  });

  it("renders an empty state rather than an empty chart frame", () => {
    const { getByText } = render(() => (
      <LineSeries result={() => ({ ...series, rows: [] })} mapping={seriesMap} />
    ));
    expect(getByText(/nothing to show/i)).toBeTruthy();
  });
});
