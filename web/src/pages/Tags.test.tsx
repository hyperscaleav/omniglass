import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Tags from "./Tags";
import { TAGS_KEY, type Tag } from "../lib/tags";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// The Tags page is a single FlatList over the /tags key registry. Data is seeded
// into the query cache so no server is needed.
//
// The identity column is the thing worth pinning here: a tag's name is a keyspace
// KEY, not a kebab segment, so the header word stays "Key", and a tag carries no
// display name at all (the API's TagBody has no such field), so the shared cell
// must render the key exactly once rather than stacking it under a repeat of
// itself or pairing it with a redundant label column.
const seed: Tag[] = [
  { id: uuidFor("tag-environment"), name: "environment", applies_to: [], propagates: true, allowed_values: ["prod", "dev"] },
  { id: uuidFor("tag-icmp-rtt-avg"), name: "icmp.rtt-avg", applies_to: ["component"], propagates: false, allowed_values: [] },
];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...TAGS_KEY], seed);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Tags />
    </QueryClientProvider>
  ));
}

describe("Tags page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists the seeded tag keys", () => {
    mount();
    expect(screen.getByText("environment")).toBeTruthy();
    expect(screen.getByText("icmp.rtt-avg")).toBeTruthy();
  });

  // "Key" rather than "Name": a dotted keyspace key is not a segment, and calling
  // it a name would teach the operator the wrong shape for what they may type.
  it("heads the identity column Key", () => {
    mount();
    const head = screen.getAllByRole("columnheader").map((th) => th.textContent?.trim());
    expect(head[0]).toContain("Key");
  });

  // A tag has no display name, so the identity cell collapses to one line. Two
  // copies of the same string in one row reads as a bug.
  it("renders each key exactly once", () => {
    mount();
    expect(screen.getAllByText("environment")).toHaveLength(1);
    expect(screen.getAllByText("icmp.rtt-avg")).toHaveLength(1);
  });

  it("keeps the governance columns beside the key", () => {
    mount();
    const head = screen.getAllByRole("columnheader").map((th) => th.textContent?.trim());
    expect(head.some((h) => h?.includes("Applies to"))).toBe(true);
    expect(head.some((h) => h?.includes("Binding"))).toBe(true);
    const row = screen.getByText("icmp.rtt-avg").closest("tr")!;
    expect(within(row).getByText("component")).toBeTruthy();
    expect(within(row).getByText("flat")).toBeTruthy();
  });

  it("shows New tag key for a caller holding tag:create", () => {
    mount(admin);
    expect(screen.getByText("New tag key")).toBeTruthy();
  });

  it("hides New tag key from a read-only viewer", () => {
    mount(viewer);
    expect(screen.queryByText("New tag key")).toBeNull();
  });
});
