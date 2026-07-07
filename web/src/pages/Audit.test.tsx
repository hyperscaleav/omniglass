import { describe, it, expect } from "vitest";
import { render } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Audit from "./Audit";
import { AUDIT_KEY, type AuditEvent } from "../lib/audit";

// The Audit page renders the trail newest-first, resolving the actor to a name
// and marking an impersonated action with the real admin behind it.
const seed: AuditEvent[] = [
  { id: "1", ts: "2026-07-07T10:00:00Z", actor: "u-alice", actor_name: "alice", verb: "login", resource: "auth" },
  { id: "2", ts: "2026-07-07T10:01:00Z", actor: "u-alice", actor_name: "alice", real_actor: "u-root", real_actor_name: "root", verb: "update", resource: "principal", resource_id: "u-alice" },
];

function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...AUDIT_KEY], seed);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Audit />
    </QueryClientProvider>
  ));
}

describe("Audit page", () => {
  it("renders events with actor, verb, and resource", () => {
    const { getByText, getAllByText } = mount();
    expect(getByText("login")).toBeTruthy();
    expect(getByText("auth")).toBeTruthy();
    expect(getAllByText("alice").length).toBeGreaterThan(0);
  });

  it("marks an impersonated action with the real actor", () => {
    const { getByText } = mount();
    // the "update" row was taken while impersonating, so it shows "as root".
    expect(getByText(/as root/)).toBeTruthy();
  });
});
