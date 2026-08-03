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
    { name: "interface_type", type: "string" },
    { name: "state", type: "string", role: "value" },
    { name: "since", type: "time", role: "time" },
  ],
  rows: states.map((s, i) => [`comp-${i}`, `if-${i}`, "icmp", s, "2026-08-03T12:00:00Z"]),
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

const feedResult: ViewResult = {
  columns: [
    { name: "ts", type: "time", role: "time" },
    { name: "owner", type: "string", role: "label" },
    { name: "event_type", type: "string" },
  ],
  rows: [["2026-08-03T12:00:00Z", "bar-1", "call.started"]],
};

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
    expect(container.querySelectorAll("thead th").length).toBe(5);
    expect(container.querySelectorAll("tbody tr").length).toBe(2);
    expect(container.querySelectorAll("tbody tr")[1].textContent).toContain("down");
  });

  // The column's declared TYPE is what makes generic formatting safe: the
  // renderer reads the contract rather than guessing from the value, so a raw
  // ISO string never reaches an operator.
  it("formats a time column as a time and humanizes the header", () => {
    const { container } = render(() => <ViewTable result={() => feedResult} />);
    const headers = [...container.querySelectorAll("thead th")].map((h) => h.textContent);
    expect(headers).toContain("event type");
    // Assert the TIME cell specifically. "some cell contains a digit" is
    // satisfied by the owner column, so it would pass against a renderer that
    // dropped the timestamp entirely.
    const cells = [...container.querySelectorAll("tbody td")].map((c) => c.textContent ?? "");
    expect(cells[0]).not.toContain("2026-08-03T12:00:00Z");
    expect(cells[0]).toMatch(/^[A-Z][a-z]{2} \d/);
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

    const beforeCells = [...container.querySelectorAll("tbody td")];
    const beforeHeaders = [...container.querySelectorAll("thead th")];

    setResult(reachability(["up", "down", "up"]));
    const after = [...container.querySelectorAll("tbody tr")];
    expect(after.length).toBe(3);
    // The unchanged rows are the SAME DOM nodes, not equal-looking new ones.
    expect(after[0]).toBe(before[0]);
    expect(after[2]).toBe(before[2]);
    // The changed row kept its node too: only the cell's text was patched.
    expect(after[1]).toBe(before[1]);
    expect(after[1].textContent).toContain("down");
    // And the CELLS survived. Every refetch parses fresh JSON, so the column
    // array is new objects each time; keying the cell loop by reference would
    // rebuild every td while the tr assertions above still passed.
    const afterCells = [...container.querySelectorAll("tbody td")];
    const afterHeaders = [...container.querySelectorAll("thead th")];
    expect(afterCells.length).toBe(beforeCells.length);
    afterCells.forEach((td, i) => expect(td).toBe(beforeCells[i]));
    afterHeaders.forEach((th, i) => expect(th).toBe(beforeHeaders[i]));
  });

  // The honest limit of the guarantee: rows are keyed by POSITION, so a
  // prepend (the shape event-feed produces, newest first) keeps every node and
  // shifts every value down. Nodes survive; "only the changed cells re-render"
  // does not hold for an insert, and the pin says so rather than implying more.
  it("keeps row nodes across a prepend, shifting values rather than rebuilding", () => {
    const [result, setResult] = createSignal<ViewResult>(reachability(["up", "down"]));
    const { container } = render(() => <ViewTable result={result} />);
    const before = [...container.querySelectorAll("tbody tr")];

    const prepended: ViewResult = {
      ...reachability([]),
      rows: [["comp-new", "if-new", "unknown", "2026-08-03T12:00:00Z"], ...(reachability(["up", "down"]).rows ?? [])],
    };
    setResult(prepended);
    const after = [...container.querySelectorAll("tbody tr")];
    expect(after.length).toBe(3);
    expect(after[0]).toBe(before[0]);
    expect(after[1]).toBe(before[1]);
    expect(after[0].textContent).toContain("comp-new");
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

  // Two interfaces on one component would otherwise render two identical
  // chips, which cannot tell an operator which one is down.
  it("distinguishes rows sharing a label using the mapped sublabel column", () => {
    const twoOnOne: ViewResult = {
      ...reachability([]),
      rows: [
        ["bar-1", "bar-1-icmp", "icmp", "up", "2026-08-03T12:00:00Z"],
        ["bar-1", "bar-1-tcp", "tcp", "down", "2026-08-03T12:00:00Z"],
      ],
    };
    const { container } = render(() => (
      <StatusGrid result={() => twoOnOne} mapping={{ ...reachabilityMap, sublabel: "interface" }} />
    ));
    const labels = [...container.querySelectorAll("[data-state]")].map((c) => c.textContent);
    expect(labels).toEqual(["bar-1 / bar-1-icmp", "bar-1 / bar-1-tcp"]);
  });

  it("labels each cell with the mapped label column", () => {
    const { getByText } = render(() => (
      <StatusGrid result={() => reachability(["up"])} mapping={reachabilityMap} />
    ));
    expect(getByText("comp-0")).toBeTruthy();
  });

  // Loud, but contained: a broken contract must fail THIS widget where an
  // operator can read why, not throw out of render and blank the console.
  it("renders a visible error naming the missing column when the mapping breaks", () => {
    const { getByRole } = render(() => (
      <StatusGrid result={() => reachability(["up"])} mapping={{ ...reachabilityMap, value: "verdict" }} />
    ));
    const alert = getByRole("alert");
    expect(alert.textContent).toContain("verdict");
    expect(alert.textContent).toContain("value");
  });

  it("renders the query's failure rather than an endless loading state", () => {
    const { getByRole } = render(() => (
      <StatusGrid
        result={() => undefined}
        mapping={reachabilityMap}
        error={() => new Error("forbidden: component:read")}
      />
    ));
    expect(getByRole("alert").textContent).toContain("forbidden");
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
      rows: [...(series.rows ?? []), ["2026-08-03T12:00:00Z", "eth1", 9], ["2026-08-03T12:01:00Z", "eth1", 10]],
    };
    const { container } = render(() => <LineSeries result={() => twoSeries} mapping={seriesMap} />);
    expect(container.querySelectorAll("polyline").length).toBe(2);
  });

  it("breaks the line at a gap rather than drawing straight through an outage", () => {
    const gapped: ViewResult = {
      ...series,
      rows: [
        ["2026-08-03T12:00:00Z", "eth0", 3],
        ["2026-08-03T12:01:00Z", "eth0", null],
        ["2026-08-03T12:02:00Z", "eth0", 4],
        ["2026-08-03T12:03:00Z", "eth0", 5],
      ],
    };
    const { container } = render(() => <LineSeries result={() => gapped} mapping={seriesMap} />);
    // Two segments, not one line interpolated across the missing reading: a
    // connected line reads as "the metric was fine the whole time".
    const drawn = container.querySelectorAll("polyline, circle");
    expect(drawn.length).toBe(2);
  });

  it("draws every series, including past the fourth", () => {
    const many: ViewResult = {
      ...series,
      rows: ["a", "b", "c", "d", "e", "f"].flatMap((k, i) => [
        ["2026-08-03T12:00:00Z", k, i + 1],
        ["2026-08-03T12:01:00Z", k, i + 2],
      ]),
    };
    const { container } = render(() => <LineSeries result={() => many} mapping={seriesMap} />);
    const lines = [...container.querySelectorAll("polyline")];
    expect(lines.length).toBe(6);
    // None of them is invisible: an opacity ramp reached zero at the fifth.
    for (const line of lines) {
      expect(line.getAttribute("stroke")).toBeTruthy();
      expect(line.getAttribute("opacity")).toBeNull();
    }
  });

  it("renders a lone reading as a point rather than a blank frame", () => {
    const one: ViewResult = { ...series, rows: [["2026-08-03T12:00:00Z", "eth0", 7]] };
    const { container } = render(() => <LineSeries result={() => one} mapping={seriesMap} />);
    expect(container.querySelectorAll("circle").length).toBe(1);
  });

  it("never plots a non-numeric reading as zero", () => {
    // sample-history carries both lanes, so a categorical row has no number.
    // Reading it as 0 would draw a dive to the axis that never happened.
    const mixed: ViewResult = {
      ...series,
      rows: [
        ["2026-08-03T12:00:00Z", "eth0", 3],
        ["2026-08-03T12:01:00Z", "eth0", null],
        ["2026-08-03T12:02:00Z", "eth0", 4],
      ],
    };
    const { container } = render(() => <LineSeries result={() => mixed} mapping={seriesMap} />);
    // Two readings, two separate marks, and the gap between them is a gap.
    const marks = [...container.querySelectorAll("polyline, circle")];
    expect(marks.length).toBe(2);
    expect(container.querySelectorAll("polyline").length).toBe(0);
  });

  it("renders an empty state rather than an empty chart frame", () => {
    const { getByText } = render(() => (
      <LineSeries result={() => ({ ...series, rows: [] })} mapping={seriesMap} />
    ));
    expect(getByText(/nothing to show/i)).toBeTruthy();
  });
});
