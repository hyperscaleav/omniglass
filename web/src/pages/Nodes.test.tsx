import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Nodes from "./Nodes";
import { NODES_KEY, type Node } from "../lib/nodes";
import { TASKS_KEY, type Task } from "../lib/tasks";
import { INTERFACES_KEY, type Interface } from "../lib/interfaces";
import { LOCATIONS_KEY, type Location } from "../lib/locations";
import { ME_KEY, type Me } from "../lib/auth";

// The Nodes page is a config over the shared FlatList: a row per collection node,
// each labelled by its display_name (the name/key is the subtitle), a row opening
// the read-edit-save blade (facts, editable identity, the derived Tasks panel, and
// Enroll / Re-enroll in the kebab), and a create Drawer. Data is seeded into the
// query cache so no server is needed; the enroll / update fetches are faked where a
// test drives them.
const now = Date.now();
const seed: Node[] = [
  { name: "edge-hq", display_name: "HQ Edge Node", location: "hq", enrolled: true, description: "HQ closet", last_heartbeat_at: new Date(now).toISOString(), tags: { environment: "prod" } }, // up
  { name: "edge-east", display_name: "East Edge", enrolled: true, last_heartbeat_at: new Date(now - 11 * 60_000).toISOString(), tags: {} }, // down (stale)
  { name: "edge-new", enrolled: false, tags: {} }, // never checked in, no display_name -> labels by key
];
const locSeed: Location[] = [
  { name: "hq", display_name: "HQ", location_type: "campus" } as Location,
  { name: "east", display_name: "East", location_type: "campus" } as Location,
];
const taskSeed: Task[] = [{ id: "t-hq", interface_id: "if-hq", mode: "poll", enabled: true, node: "edge-hq" }];
const ifaceSeed: Interface[] = [{ id: "if-hq", name: "disp-1-tcp", type: "tcp", component: "disp-1", node: "edge-hq" }];

const owner: Me = { principal: { id: "p", kind: "human" }, permissions: [">"], grants: [] };
const reader: Me = { principal: { id: "r", kind: "human" }, permissions: ["node:read"], grants: [] };

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function mount(me: Me) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...NODES_KEY], seed);
  qc.setQueryData([...LOCATIONS_KEY], locSeed);
  qc.setQueryData([...TASKS_KEY], taskSeed);
  qc.setQueryData([...INTERFACES_KEY], ifaceSeed);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Nodes />
    </QueryClientProvider>
  ));
}

describe("Nodes page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("labels each row by display_name (key as subtitle), falling back to the key, with the status pill", () => {
    const { getByText, getAllByText } = mount(owner);
    expect(getByText("HQ Edge Node")).toBeTruthy(); // display_name label
    expect(getByText(/edge-hq/)).toBeTruthy(); // key + location in the subtitle
    expect(getByText("East Edge")).toBeTruthy();
    // A node with no display_name falls back to its key: it reads as both the label
    // and the subtitle, so the key appears twice.
    expect(getAllByText("edge-new").length).toBe(2);
    // Status is derived from last_heartbeat_at against the down window.
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

  it("gives node:update an Edit action that edits display_name, location, and description", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const url = typeof input === "string" ? input : req.url;
      const method = typeof input === "string" ? "GET" : req.method;
      if (url.includes("/nodes/edge-hq") && method === "PATCH") {
        return json({ name: "edge-hq", display_name: "HQ Prod", location: "east", enrolled: true });
      }
      return json({ nodes: seed });
    });

    mount(owner);
    fireEvent.click(screen.getByText("HQ Edge Node"));
    const blade = await screen.findByRole("dialog");
    // Read mode shows the identity; the name (key) is present but not an input.
    expect(within(blade).getByText("HQ closet")).toBeTruthy();

    fireEvent.click(within(blade).getByLabelText("Edit"));
    // The display-name input carries the current value; the name is not editable.
    const nameField = within(blade).getByDisplayValue("HQ Edge Node");
    fireEvent.input(nameField, { target: { value: "HQ Prod" } });
    fireEvent.click(within(blade).getByText("Save"));

    await waitFor(() => {
      const patched = vi.mocked(fetch).mock.calls.find(([input]) => (input as Request)?.method === "PATCH");
      expect(patched).toBeTruthy();
    });
  });

  it("hides Edit and Re-enroll for a reader (no node:update / node:enroll)", async () => {
    mount(reader);
    fireEvent.click(screen.getByText("HQ Edge Node"));
    const blade = await screen.findByRole("dialog");
    await within(blade).findByText("Enrolled");
    expect(within(blade).queryByLabelText("Edit")).toBeNull();
    expect(within(blade).queryByText(/Re-?enroll/i)).toBeNull();
  });

  it("folds the node's derived tasks into a read-only panel on the detail blade", async () => {
    mount(owner);
    fireEvent.click(screen.getByText("HQ Edge Node"));
    const blade = await screen.findByRole("dialog");
    await within(blade).findByText("Tasks");
    expect(await within(blade).findByText("disp-1-tcp")).toBeTruthy();
    expect(within(blade).getByText("reachability")).toBeTruthy();
    expect(within(blade).getByText(/driver fn soon/i)).toBeTruthy();
    expect(within(blade).getByText("enabled")).toBeTruthy();
    expect(within(blade).queryByText("poll")).toBeNull(); // the mode mechanism is not shown
  });

  it("re-enrolls from the kebab and reveals the token once, copying it and clearing on close", async () => {
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
    fireEvent.click(screen.getByText("HQ Edge Node"));
    const blade = await screen.findByRole("dialog");
    // Re-enroll is a secondary action in the kebab, not the primary slot.
    fireEvent.click(within(blade).getByLabelText("More actions"));
    fireEvent.click(within(blade).getByText("Re-enroll"));

    expect(await screen.findByDisplayValue("og_tok_SECRET")).toBeTruthy();
    expect(screen.getByText(/shown once/i)).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /copy/i }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith("og_tok_SECRET"));

    fireEvent.click(screen.getByText("Done"));
    await waitFor(() => expect(screen.queryByDisplayValue("og_tok_SECRET")).toBeNull());
  });

  it("creates a node with its identity fields then auto-enrolls it, revealing the token", async () => {
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
    // The create form also carries display_name + location (parity with components).
    expect(screen.getByLabelText("Display name")).toBeTruthy();
    expect(screen.getByLabelText("Location")).toBeTruthy();
    fireEvent.click(screen.getByText("Create node"));

    expect(await screen.findByDisplayValue("og_new_TOKEN")).toBeTruthy();
    await waitFor(() => expect(screen.queryByText("Create node")).toBeNull());
  });
});
