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
// The identity column is the thing worth pinning here: a tag carries no display
// name at all (the API's TagBody has no such field), so the shared cell must render
// the name exactly once rather than stacking it under a repeat of itself or pairing
// it with a redundant label column.
const seed: Tag[] = [
  { id: uuidFor("tag-environment"), name: "environment", applies_to: [], propagates: true, allowed_values: ["prod", "dev"] },
  { id: uuidFor("tag-icmp-rtt-avg"), name: "icmp-rtt-avg", applies_to: ["component"], propagates: false, allowed_values: [] },
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

  it("lists the seeded tags", () => {
    mount();
    expect(screen.getByText("environment")).toBeTruthy();
    expect(screen.getByText("icmp-rtt-avg")).toBeTruthy();
  });

  // One header word everywhere. A tag's name is validated as a dotted keyspace key
  // rather than kebab, which is a validation difference and not a second concept.
  it("heads the identity column Name", () => {
    mount();
    const head = screen.getAllByRole("columnheader").map((th) => th.textContent?.trim());
    expect(head[0]).toContain("Name");
  });

  // A tag has no label, so the identity cell collapses to one line. Two
  // copies of the same string in one row reads as a bug.
  it("renders each name exactly once", () => {
    mount();
    expect(screen.getAllByText("environment")).toHaveLength(1);
    expect(screen.getAllByText("icmp-rtt-avg")).toHaveLength(1);
  });

  it("keeps the governance columns beside the key", () => {
    mount();
    const head = screen.getAllByRole("columnheader").map((th) => th.textContent?.trim());
    expect(head.some((h) => h?.includes("Applies to"))).toBe(true);
    expect(head.some((h) => h?.includes("Binding"))).toBe(true);
    const row = screen.getByText("icmp-rtt-avg").closest("tr")!;
    expect(within(row).getByText("component")).toBeTruthy();
    expect(within(row).getByText("flat")).toBeTruthy();
  });

  it("shows New tag for a caller holding tag:create", () => {
    mount(admin);
    expect(screen.getByText("New tag")).toBeTruthy();
  });

  it("hides New tag from a read-only viewer", () => {
    mount(viewer);
    expect(screen.queryByText("New tag")).toBeNull();
  });
});
