import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Nodes from "./Nodes";
import { NODES_KEY, type Node } from "../lib/nodes";
import { ME_KEY, type Me } from "../lib/auth";

// The Nodes page is a config over the shared FlatList: a row per collection node,
// a row opening the side Drawer detail (facts + an Enroll / Re-enroll action), and
// a create Drawer that mints the enrollment token. Data is seeded into the query
// cache so no server is needed; the enrollment fetch is faked where a test drives
// the token modal.
const now = Date.now();
const seed: Node[] = [
  { name: "edge-hq", enrolled: true, description: "HQ closet", last_heartbeat_at: new Date(now).toISOString() }, // up
  { name: "edge-east", enrolled: true, last_heartbeat_at: new Date(now - 11 * 60_000).toISOString() }, // down (stale)
  { name: "edge-new", enrolled: false }, // never checked in
];

const owner: Me = { principal: { id: "p", kind: "human" }, permissions: [">"], grants: [] };
const reader: Me = { principal: { id: "r", kind: "human" }, permissions: ["node:read"], grants: [] };

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function mount(me: Me) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...NODES_KEY], seed);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Nodes />
    </QueryClientProvider>
  ));
}

describe("Nodes page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders a row per node with the client-derived status pill (up / down / never)", () => {
    const { getByText } = mount(owner);
    expect(getByText("edge-hq")).toBeTruthy();
    expect(getByText("edge-east")).toBeTruthy();
    expect(getByText("edge-new")).toBeTruthy();
    // Status is derived from last_heartbeat_at against the down window, not a
    // fabricated field: fresh -> up, stale -> down, never-seen -> never.
    expect(getByText("up")).toBeTruthy();
    expect(getByText("down")).toBeTruthy();
    expect(getByText("never")).toBeTruthy();
  });

  it("hides the create affordance without node:create, shows it with", () => {
    const { queryByText, unmount } = mount(reader);
    expect(queryByText("New node")).toBeNull();
    unmount();
    mount(owner);
    expect(screen.getByText("New node")).toBeTruthy();
  });

  it("hides the Re-enroll action in the detail drawer without node:enroll", async () => {
    mount(reader);
    fireEvent.click(screen.getByText("edge-hq"));
    // Wait for the detail drawer to render (a fact always present) before
    // asserting the gated action is absent, not just not-yet-rendered.
    await screen.findByText("Enrolled");
    expect(screen.queryByText(/Re-?enroll/i)).toBeNull();
  });

  it("reveals the enrollment token once, copies it to the clipboard, and clears it on close", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const url = typeof input === "string" ? input : req.url;
      const method = typeof input === "string" ? "GET" : req.method;
      if (url.includes(":enroll") && method === "POST") return json({ name: "edge-hq", token: "og_tok_SECRET" });
      return json({ nodes: seed });
    });

    mount(owner);
    // Open the detail Drawer and re-enroll (edge-hq is already enrolled).
    fireEvent.click(screen.getByText("edge-hq"));
    fireEvent.click(await screen.findByText("Re-enroll"));

    // The show-once modal reveals the token and the once-only warning.
    expect(await screen.findByDisplayValue("og_tok_SECRET")).toBeTruthy();
    expect(screen.getByText(/shown once/i)).toBeTruthy();

    // Copy calls the clipboard with exactly the token.
    fireEvent.click(screen.getByRole("button", { name: /copy/i }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith("og_tok_SECRET"));

    // Close clears the token from the DOM (it does not outlive the modal).
    fireEvent.click(screen.getByText("Done"));
    await waitFor(() => expect(screen.queryByDisplayValue("og_tok_SECRET")).toBeNull());
  });

  it("creates a node then auto-enrolls it, revealing the token in the show-once modal", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const url = typeof input === "string" ? input : req.url;
      const method = typeof input === "string" ? "GET" : req.method;
      if (url.includes(":enroll") && method === "POST") return json({ name: "edge-2", token: "og_new_TOKEN" });
      if (url.endsWith("/nodes") && method === "POST") return json({ name: "edge-2", enrolled: false }, 201);
      return json({ nodes: [...seed, { name: "edge-2", enrolled: true }] });
    });

    mount(owner);
    fireEvent.click(screen.getByText("New node"));
    const nameInput = (await screen.findByLabelText("Name")) as HTMLInputElement;
    fireEvent.input(nameInput, { target: { value: "edge-2" } });
    fireEvent.click(screen.getByText("Create node"));

    // The create Drawer gives way to the show-once token modal for the new node.
    expect(await screen.findByDisplayValue("og_new_TOKEN")).toBeTruthy();
    await waitFor(() => expect(screen.queryByText("Create node")).toBeNull());
  });
});
