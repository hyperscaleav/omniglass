import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Keys from "./Keys";
import { KEYS_KEY, type KeyRow } from "../lib/keys";
import { ME_KEY, type Me } from "../lib/auth";

// The Keys page is a single FlatList over the /keys catalog. Official (seed-owned)
// keys are read-only; a custom key is writable only when the caller holds
// key:create / key:update. Data is seeded into the query cache so no server is
// needed.
const seed: KeyRow[] = [
  { name: "serial_number", data_type: "string", display_name: "Serial number", official: true },
  { name: "icmp.reachable", data_type: "int", display_name: "ICMP Reachable", kind: "metric", official: true },
  { name: "rack_unit", data_type: "int", display_name: "Rack unit", official: false },
];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...KEYS_KEY], seed);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Keys />
    </QueryClientProvider>
  ));
}

describe("Keys page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists the seeded keys", () => {
    mount();
    expect(screen.getByText("serial_number")).toBeTruthy();
    expect(screen.getByText("icmp.reachable")).toBeTruthy();
    expect(screen.getByText("rack_unit")).toBeTruthy();
  });

  it("shows New key for a caller holding key:create", () => {
    mount(admin);
    expect(screen.getByText("New key")).toBeTruthy();
  });

  it("hides New key from a read-only viewer", () => {
    mount(viewer);
    expect(screen.queryByText("New key")).toBeNull();
  });
});
