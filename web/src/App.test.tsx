import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import { render, screen, cleanup, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import App from "./App";
import Page from "./components/Page";

// The page title reads once: a page wearing the <Page> header owns it, so the
// top bar drops the section label's title and keeps the controls. A route with
// no <Page> header keeps its title in the top bar.

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
  it("shows the title once, as the page heading, for a route with a Page header", () => {
    shell(() => <Page route="/roles"><p>body</p></Page>);
    expect(screen.getAllByRole("heading", { name: "Roles" })).toHaveLength(1);
    const bar = screen.getByRole("banner");
    expect(within(bar).queryByText(/Roles/)).toBeNull();
    expect(within(bar).getByRole("button", { name: "Search and jump" })).toBeTruthy();
  });

  it("keeps the title in the top bar for a route with no Page header", () => {
    shell(() => <p>bare</p>);
    expect(within(screen.getByRole("banner")).getByText(/Roles/)).toBeTruthy();
  });
});
