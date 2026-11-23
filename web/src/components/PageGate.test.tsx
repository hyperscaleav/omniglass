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
function mount(queryFn: () => Promise<unknown>, retryDelay?: number, retry: typeof queryRetry | number = queryRetry) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry, ...(retryDelay === undefined ? {} : { retryDelay }) } } });
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
    }); // the app's real backoff (1s, 2s, 4s): a retried 403 would hold the spinner for seconds
    expect(await screen.findByText("You do not have access to Settings", {}, { timeout: 500 })).toBeTruthy();
    expect(document.querySelector(".loading")).toBeNull();
    expect(screen.queryByText("page body")).toBeNull();
    expect(screen.queryByRole("button", { name: /retry/i })).toBeNull();
    expect(calls).toBe(1); // a refusal is not retried
  });

  it("holds a spinner while the first read is in flight", async () => {
    mount(() => new Promise(() => {}), 1);
    expect(await screen.findByTestId("page-pending")).toBeTruthy();
  });

  it("renders the page once the read lands", async () => {
    mount(async () => ({ ok: true }), 1);
    expect(await screen.findByText("page body")).toBeTruthy();
  });

  it("offers Retry on an ordinary failure, not the denied face", async () => {
    mount(async () => {
      throw new Error("boom");
    }, 1);
    expect(await screen.findByRole("button", { name: /retry/i })).toBeTruthy();
    await waitFor(() => expect(screen.queryByText(/do not have access/i)).toBeNull());
  });
});

// The regression this bug started from: with the library default (retry 3) a 403
// is retried through its backoff and the page stays pending. Kept as a test of the
// test: the old policy must NOT satisfy the assertion above.
describe("PageGate under the old retry policy", () => {
  it("leaves the spinner up through the backoff, which is the bug", async () => {
    mount(async () => {
      throw deniedError({ status: 403 });
    }, undefined, 3);
    await new Promise((r) => setTimeout(r, 300));
    expect(screen.queryByText("You do not have access to Settings")).toBeNull();
    expect(screen.getByTestId("page-pending")).toBeTruthy();
  });
});
