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
// The identity column is the thing worth pinning here. A tag gained a label in
// #613, so the shared cell stacks the label over the key on a labelled row and
// collapses to the key alone on one without: two copies of the same string in
// one row reads as a bug, and so does a second "Label" column beside the first.
// In the order ListTags serves them: by label, the unlabelled keys last, the
// name breaking ties (`order by label nulls last, name`, #613).
const seed: Tag[] = [
  { id: uuidFor("tag-environment"), name: "environment", label: "Environment", applies_to: [], propagates: true, allowed_values: ["prod", "dev"] },
  // Named so that a NAME sort would put it first: it comes after the labelled
  // key anyway, because it carries no label.
  { id: uuidFor("tag-aaa-region"), name: "aaa-region", applies_to: [], propagates: true, allowed_values: [] },
  { id: uuidFor("tag-icmp-rtt-avg"), name: "icmp-rtt-avg", applies_to: ["component"], propagates: false, allowed_values: [] },
];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

function mount(me: Me = admin, rows: Tag[] = seed) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...TAGS_KEY], rows);
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

  // One cell, two lines, no repetition: the label over the key it belongs to.
  it("renders each name exactly once, under its label where there is one", () => {
    mount();
    expect(screen.getAllByText("environment")).toHaveLength(1);
    expect(screen.getAllByText("icmp-rtt-avg")).toHaveLength(1);
    const row = screen.getByText("Environment").closest("tr")!;
    expect(within(row).getByText("environment")).toBeTruthy();
    expect(screen.queryByRole("columnheader", { name: /label/i })).toBeNull();
  });

  // The name renders VERBATIM on a key with no label: nothing re-cases
  // `icmp-rtt-avg` into "Icmp Rtt Avg" to fill the gap (#613).
  it("falls back to the key verbatim where a tag has no label", () => {
    mount();
    const row = screen.getByText("icmp-rtt-avg").closest("tr")!;
    expect(within(row).queryByText(/Icmp/)).toBeNull();
  });

  // The server's order is the order (#757). ListTags already orders by label,
  // unlabelled last, so the page renders the rows as received: a page that sorted
  // again would put "Alpha" ahead of "Zulu" here, and a second comparator is a
  // second place for the rule to drift.
  it("renders the rows in the order the server sent them, without re-sorting", () => {
    const sent: Tag[] = [
      { id: uuidFor("tag-zulu"), name: "zulu", label: "Zulu", applies_to: [], propagates: true, allowed_values: [] },
      { id: uuidFor("tag-alpha"), name: "alpha", label: "Alpha", applies_to: [], propagates: true, allowed_values: [] },
    ];
    mount(admin, sent);
    const cells = screen.getAllByRole("row").slice(1).map((tr) => tr.textContent ?? "");
    expect(cells[0]).toContain("Zulu");
    expect(cells[1]).toContain("Alpha");
  });

  // The server's order puts an unlabelled key below a labelled one; the page
  // keeps it there.
  it("keeps an unlabelled key below a labelled one", () => {
    mount();
    const cells = screen.getAllByRole("row").slice(1).map((tr) => tr.textContent ?? "");
    const labelled = cells.findIndex((c) => c.includes("Environment"));
    const unlabelled = cells.findIndex((c) => c.includes("aaa-region"));
    expect(labelled).toBeGreaterThanOrEqual(0);
    expect(unlabelled).toBeGreaterThan(labelled);
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
