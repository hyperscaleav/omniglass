import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import { render, screen, fireEvent, cleanup } from "@solidjs/testing-library";
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
        <Route path="*" component={() => <TopBar section="Overview" onOpenPalette={() => {}} onOpenOmnibot={onOpenOmnibot} />} />
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
