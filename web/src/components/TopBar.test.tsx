import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import { render, screen, fireEvent, cleanup, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import TopBar from "./TopBar";

// The top bar opens omnibot's panel (#1167) only for a person the shell says
// may run it: no handler, no button.

beforeEach(() => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async () => new Response(JSON.stringify({ items: [] }), { headers: { "Content-Type": "application/json" } }));
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function bar(onOpenOmnibot?: () => void) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={() => <TopBar section="Admin" title="Overview" onOpenPalette={() => {}} onOpenOmnibot={onOpenOmnibot} />} />
      </Router>
    </QueryClientProvider>
  ));
}

describe("the top bar's omnibot button", () => {
  it("opens the panel", () => {
    const opened = vi.fn();
    bar(opened);
    fireEvent.click(screen.getByRole("button", { name: "Ask omnibot" }));
    expect(opened).toHaveBeenCalledOnce();
  });

  it("is not there for someone who may not run omnibot", () => {
    bar();
    expect(screen.queryByRole("button", { name: "Ask omnibot" })).toBeNull();
  });
});

describe("TopBar trail (#2575)", () => {
  const crumbs = (n: number) => Array.from({ length: n }, (_, i) => ({ key: `c${i}`, label: `Place number ${i}`, href: `/locations/c${i}` }));
  const mount = (n: number) =>
    render(() => (
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <Router>
          <Route path="*" component={() => <TopBar section="" title="Display 1" trail={crumbs(n)} onOpenPalette={() => {}} />} />
        </Router>
      </QueryClientProvider>
    ));

  it("shows a short trail whole, each crumb a link, before the one h1", () => {
    mount(2);
    const trail = screen.getByTestId("breadcrumb");
    expect(within(trail).getAllByRole("link").map((a) => a.textContent)).toEqual(["Place number 0", "Place number 1"]);
    expect(screen.queryByRole("button", { name: "Show hidden path" })).toBeNull();
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Display 1");
  });

  it("collapses the middle of a long trail to an ellipsis menu that lists them as links", async () => {
    mount(6);
    const trail = screen.getByTestId("breadcrumb");
    expect(within(trail).getAllByRole("link").map((a) => a.textContent)).toEqual(["Place number 0", "Place number 5"]);
    fireEvent.click(screen.getByRole("button", { name: "Show hidden path" }));
    const menu = await screen.findByTestId("trail-menu");
    expect(within(menu).getAllByRole("link").map((a) => a.textContent)).toEqual(["Place number 1", "Place number 2", "Place number 3", "Place number 4"]);
  });
});
