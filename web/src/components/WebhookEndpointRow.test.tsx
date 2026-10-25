import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import ReachabilityPanel from "./ReachabilityPanel";
import { REACHABILITY_KEY, type Reachability } from "../lib/reachability";
import { ME_KEY, type Me } from "../lib/auth";
import * as secrets from "../lib/secrets";

// A webhook endpoint (#1024) is a push endpoint: nothing is dialed, so its row
// carries no ladder and no verdict. It reads last heard, its scheme (or
// unsigned), and what its scheme refused; expanded, the URL a sender posts to,
// its token masked, revealed and copied only under secret:reveal.
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

function hook(over: Partial<NonNullable<Reachability["endpoints"][number]["webhook"]>> = {}): Reachability {
  return {
    component: "ucc-1",
    endpoints: [{
      id: "ep-1", endpoint: "webhook", transport: "webhook", label: "Meeting webhooks",
      last_seen: ago(90_000), verdict: null, layers: [], history: [],
      webhook: { path: "/webhooks/••••••••", scheme: "hmac", signed: true, secret: "c-1/webhook-token", ...over },
    }],
  };
}

function mount(data: Reachability, permissions: string[]) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...REACHABILITY_KEY(data.component)], data);
  qc.setQueryData([...ME_KEY], { principal: { id: "p", kind: "human" }, permissions, grants: [] } as Me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <ReachabilityPanel name={data.component} />
    </QueryClientProvider>
  ));
}

describe("a webhook endpoint's row", () => {
  it("reads last heard and its scheme, never a verdict or a ladder", () => {
    const { getByText, queryByText } = mount(hook(), []);
    expect(getByText(/last heard/).textContent).toMatch(/1m|90s|2m/);
    expect(getByText("hmac")).toBeTruthy();
    expect(getByText("push")).toBeTruthy();
    for (const word of ["responding", "down", "stale", "unknown"]) expect(queryByText(word)).toBeNull();
  });

  it("marks an unsigned endpoint and counts its refusals", () => {
    const { getByText } = mount(hook({ scheme: "none", signed: false, refused: { count: 3, at: ago(5_000), reason: "signature mismatch", source: "203.0.113.9" } }), []);
    expect(getByText("unsigned")).toBeTruthy();
    const refused = getByText("3 refused");
    expect(refused.getAttribute("title")).toContain("signature mismatch");
    expect(refused.getAttribute("title")).toContain("203.0.113.9");
  });

  it("never heard reads so, not down", () => {
    const data = hook();
    data.endpoints[0].last_seen = undefined;
    const { getByText } = mount(data, []);
    expect(getByText("never heard")).toBeTruthy();
  });

  it("reveals the URL under secret:reveal", async () => {
    const reveal = vi.spyOn(secrets, "revealSecret").mockResolvedValue({ token: "TOKEN22CHARSxxxxxxxxxx" });
    const { getByText, getByRole } = mount(hook(), ["secret:reveal"]);
    fireEvent.click(getByText("Meeting webhooks"));
    expect(getByText(/\/api\/v1\/webhooks\/•+/)).toBeTruthy();
    fireEvent.click(getByRole("button", { name: "Reveal" }));
    await waitFor(() => expect(getByText(/\/api\/v1\/webhooks\/TOKEN22CHARSxxxxxxxxxx/)).toBeTruthy());
    expect(reveal).toHaveBeenCalledWith("c-1/webhook-token");
    reveal.mockRestore();
  });

  it("keeps the token masked without secret:reveal, naming the permission", () => {
    const { getByText, queryByRole } = mount(hook(), []);
    fireEvent.click(getByText("Meeting webhooks"));
    expect(queryByRole("button", { name: "Reveal" })).toBeNull();
    expect(getByText(/Requires secret:reveal/)).toBeTruthy();
  });
});
