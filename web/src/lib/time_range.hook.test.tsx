import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@solidjs/testing-library";
import { Route, Router, useLocation } from "@solidjs/router";
import { useTimeRange } from "./time_range";

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
