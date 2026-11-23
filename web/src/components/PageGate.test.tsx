import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen, waitFor } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/solid-query";
import PageGate from "./PageGate";
import { deniedError } from "../api/client";
import { queryRetry } from "../lib/queryDefaults";

afterEach(cleanup);

// The retry policy is the one the app installs (lib/queryDefaults), with the delay
// zeroed so a retried failure settles inside the test: a 403 must settle at once,
// an ordinary failure after its retries.
function mount(queryFn: () => Promise<unknown>) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: queryRetry, retryDelay: 1 } } });
  function Page() {
    const q = useQuery(() => ({ queryKey: ["page"], queryFn }));
    return (
      <PageGate query={q} section="Settings" needs={["settings:read"]}>
        <p>page body</p>
      </PageGate>
    );
  }
  window.history.pushState({}, "", "/web/x");
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router base="/web">
        <Route path="*" component={Page} />
      </Router>
    </QueryClientProvider>
  ));
}

describe("PageGate", () => {
  it("shows the denied face on a 403 and never leaves a spinner (#2442)", async () => {
    let calls = 0;
    mount(async () => {
      calls++;
      throw deniedError({ title: "Forbidden", status: 403, detail: "forbidden" });
    });
    expect(await screen.findByText("You do not have access to Settings")).toBeTruthy();
    expect(document.querySelector(".loading")).toBeNull();
    expect(screen.queryByText("page body")).toBeNull();
    expect(screen.queryByRole("button", { name: /retry/i })).toBeNull();
    expect(calls).toBe(1); // a refusal is not retried
  });

  it("holds a spinner while the first read is in flight", async () => {
    mount(() => new Promise(() => {}));
    expect(await screen.findByTestId("page-pending")).toBeTruthy();
  });

  it("renders the page once the read lands", async () => {
    mount(async () => ({ ok: true }));
    expect(await screen.findByText("page body")).toBeTruthy();
  });

  it("offers Retry on an ordinary failure, not the denied face", async () => {
    mount(async () => {
      throw new Error("boom");
    });
    expect(await screen.findByRole("button", { name: /retry/i })).toBeTruthy();
    await waitFor(() => expect(screen.queryByText(/do not have access/i)).toBeNull());
  });
});
