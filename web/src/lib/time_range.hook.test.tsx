import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@solidjs/testing-library";
import { Route, Router, useLocation } from "@solidjs/router";
import { useHostTimeRange, useTimeRange } from "./time_range";

// useTimeRange over the router's search params (#2304): it reads what the
// address says and writes the range back, keeping every other param.
function Probe() {
  const { range, setRange } = useTimeRange();
  const loc = useLocation();
  return (
    <div>
      <span data-testid="range">{JSON.stringify(range())}</span>
      <span data-testid="search">{loc.search}</span>
      <button onClick={() => setRange({ kind: "preset", preset: "7d" })}>7d</button>
      <button onClick={() => setRange({ kind: "absolute", from: Date.parse("2026-11-01T08:00:00Z"), to: Date.parse("2026-11-01T09:00:00Z") })}>abs</button>
      <button onClick={() => setRange({ kind: "preset", preset: "24h" })}>24h</button>
    </div>
  );
}

const mount = (path: string) => {
  window.history.pushState({}, "", path);
  render(() => (
    <Router base="/web">
      <Route path="/p" component={Probe} />
    </Router>
  ));
};

afterEach(cleanup);

describe("useTimeRange", () => {
  // ?from= also names the template a row was created from (#1329): moving
  // between presets leaves it alone; only an absolute range owns from and to.
  it("moving between presets keeps a template's ?from=", async () => {
    mount("/web/p?from=mr55-system");
    fireEvent.click(screen.getByText("7d"));
    await screen.findByText("?from=mr55-system&range=7d");
  });

  it("reads the range the address carries", () => {
    mount("/web/p?range=6h");
    expect(screen.getByTestId("range").textContent).toBe(JSON.stringify({ kind: "preset", preset: "6h" }));
  });

  it("writes a preset, an absolute range and the default back, keeping the tab", async () => {
    mount("/web/p?tab=activity");
    fireEvent.click(screen.getByText("7d"));
    await screen.findByText("?tab=activity&range=7d");
    fireEvent.click(screen.getByText("abs"));
    await screen.findByText("?tab=activity&from=2026-11-01T08%3A00%3A00.000Z&to=2026-11-01T09%3A00%3A00.000Z");
    expect(JSON.parse(screen.getByTestId("range").textContent!).kind).toBe("absolute");
    fireEvent.click(screen.getByText("24h"));
    await screen.findByText("?tab=activity");
  });
});

// A blade's range (#2425, ADR-2423): it opens on the page's range but is its
// own, so picking one in a blade never rewrites the page's address.
function BladeProbe() {
  const { range, setRange } = useHostTimeRange("blade");
  const loc = useLocation();
  return (
    <div>
      <span data-testid="range">{JSON.stringify(range())}</span>
      <span data-testid="search">{loc.search}</span>
      <button onClick={() => setRange({ kind: "preset", preset: "7d" })}>7d</button>
    </div>
  );
}

describe("useHostTimeRange", () => {
  it("a blade opens on the page's range and keeps its own picks out of the address", async () => {
    window.history.pushState({}, "", "/web/b?range=6h");
    render(() => (
      <Router base="/web">
        <Route path="/b" component={BladeProbe} />
      </Router>
    ));
    expect(screen.getByTestId("range").textContent).toBe(JSON.stringify({ kind: "preset", preset: "6h" }));
    fireEvent.click(screen.getByText("7d"));
    await screen.findByText(JSON.stringify({ kind: "preset", preset: "7d" }));
    expect(screen.getByTestId("search").textContent).toBe("?range=6h");
  });
});

// The range given, apart from the default (#2741): a body whose tabs open on
// different defaults reads `given`, and writes every pick to the address.
function GivenProbe(props: { host: "page" | "blade" }) {
  const { given, setRange } = useHostTimeRange(props.host, { explicit: true });
  const loc = useLocation();
  return (
    <div>
      <span data-testid="given">{JSON.stringify(given() ?? null)}</span>
      <span data-testid="search">{loc.search}</span>
      <button onClick={() => setRange({ kind: "preset", preset: "24h" })}>24h</button>
    </div>
  );
}

describe("useHostTimeRange's given range (#2741)", () => {
  const mountGiven = (path: string, host: "page" | "blade") => {
    window.history.pushState({}, "", path);
    render(() => (
      <Router base="/web">
        <Route path="/g" component={() => <GivenProbe host={host} />} />
      </Router>
    ));
  };

  it("a page with no range in the address has none given, and an explicit pick of 24 hours lands in it", async () => {
    mountGiven("/web/g?tab=activity", "page");
    expect(screen.getByTestId("given").textContent).toBe("null");
    fireEvent.click(screen.getByText("24h"));
    await screen.findByText("?tab=activity&range=24h");
    expect(screen.getByTestId("given").textContent).toBe(JSON.stringify({ kind: "preset", preset: "24h" }));
  });

  it("a blade opens with none given when its page has none, and a pick is given after", async () => {
    mountGiven("/web/g", "blade");
    expect(screen.getByTestId("given").textContent).toBe("null");
    fireEvent.click(screen.getByText("24h"));
    await screen.findByText(JSON.stringify({ kind: "preset", preset: "24h" }));
    expect(screen.getByTestId("search").textContent).toBe("");
  });
});
