import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import { render, screen, cleanup, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import App from "./App";
import Page from "./components/Page";

// The page title reads once, in the top bar (#2575): a page wearing the <Page> header feeds it its title, so the
// top bar renders it as the one h1 with the hint beside it. A route with
// no <Page> header gets its label as the same h1.

beforeEach(() => {
  window.history.pushState({}, "", "/roles");
  vi.spyOn(globalThis, "fetch").mockImplementation(async () => new Response(JSON.stringify({ items: [] }), { headers: { "Content-Type": "application/json" } }));
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function shell(body: () => any) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router root={App}>
        <Route path="*" component={body} />
      </Router>
    </QueryClientProvider>
  ));
}

describe("the page title is not repeated in the top bar", () => {
  it("shows the title once, as the one h1 in the top bar, for a route with a Page header", () => {
    shell(() => <Page route="/roles"><p>body</p></Page>);
    expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
    const bar = screen.getByRole("banner");
    expect(within(bar).getByRole("heading", { level: 1 }).textContent).toBe("Roles");
    expect(within(bar).getByText(/Admin/)).toBeTruthy();
    expect(within(bar).getByText(/built-in/i)).toBeTruthy();
    expect(within(bar).getByRole("button", { name: "Search and jump" })).toBeTruthy();
  });

  it("keeps the title in the top bar for a route with no Page header", () => {
    shell(() => <p>bare</p>);
    expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
    expect(within(screen.getByRole("banner")).getByRole("heading", { level: 1 }).textContent).toBe("Roles");
  });
});
