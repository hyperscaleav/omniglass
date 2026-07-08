import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Interfaces from "./Interfaces";
import { INTERFACES_KEY, type Interface } from "../lib/interfaces";
import { COMPONENTS_KEY } from "../lib/components";
import { NODES_KEY } from "../lib/nodes";
import { ME_KEY, type Me } from "../lib/auth";

// The Interfaces page is a config over the shared FlatList: a row per interface, a
// row opening the side Drawer detail (facts + inline edit + delete), and a create
// Drawer. Data is seeded into the query cache so no server is needed.
const seed: Interface[] = [
  { name: "disp-1-tcp", type: "tcp", component: "disp-1", node: "edge-hq", params: { target: "10.0.0.1:22" } },
  { name: "disp-1-icmp", type: "icmp", component: "disp-1", params: { target: "10.0.0.1" } },
  { name: "srv-tcp", type: "tcp", params: { target: "10.0.0.9:80" } },
];

const owner: Me = { principal: { id: "p", kind: "human" }, permissions: [">"], grants: [] };
const reader: Me = { principal: { id: "r", kind: "human" }, permissions: ["interface:read"], grants: [] };

function mount(me: Me) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...INTERFACES_KEY], seed);
  qc.setQueryData([...COMPONENTS_KEY], [{ id: "c1", name: "disp-1", component_type: "display" }]);
  qc.setQueryData([...NODES_KEY], [{ name: "edge-hq", enrolled: true }, { name: "edge-east", enrolled: true }]);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Interfaces />
    </QueryClientProvider>
  ));
}

describe("Interfaces page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders a row per interface with its type, component, node, and target", () => {
    const { getByText, getAllByText } = mount(owner);
    expect(getByText("disp-1-tcp")).toBeTruthy();
    expect(getByText("disp-1-icmp")).toBeTruthy();
    expect(getByText("srv-tcp")).toBeTruthy();
    // type badges (tcp appears on two rows), the target, and the server-hosted marker.
    expect(getAllByText("tcp").length).toBeGreaterThan(0);
    expect(getByText("10.0.0.1:22")).toBeTruthy();
    expect(getByText("server-hosted")).toBeTruthy(); // srv-tcp has no component
  });

  it("hides the create affordance without interface:create, shows it with", () => {
    const { queryByText, unmount } = mount(reader);
    expect(queryByText("New interface")).toBeNull();
    unmount();
    mount(owner);
    expect(screen.getByText("New interface")).toBeTruthy();
  });

  it("hides Edit and Delete in the detail Drawer without update/delete perms", async () => {
    mount(reader);
    fireEvent.click(screen.getByText("disp-1-tcp"));
    // Wait for the detail Drawer (a dialog) to render before asserting the gated
    // actions are absent, not just not-yet-rendered.
    await screen.findByRole("dialog");
    expect(screen.queryByText("Edit")).toBeNull();
    expect(screen.queryByText("Delete")).toBeNull();
  });

  it("opens the create Drawer and offers only the built types (icmp, tcp)", async () => {
    mount(owner);
    fireEvent.click(screen.getByText("New interface"));
    const typeSelect = (await screen.findByLabelText("Type")) as HTMLSelectElement;
    const options = Array.from(typeSelect.options).map((o) => o.value);
    expect(options).toEqual(["icmp", "tcp"]);
  });

  it("posts the create body (type + component + node + params.target) and lands on the new row", async () => {
    const created: Interface = { name: "disp-1-tcp2", type: "tcp", component: "disp-1", node: "edge-east", params: { target: "10.0.0.2:80" } };
    const calls: { url: string; method: string; body: unknown }[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const url = typeof input === "string" ? input : req.url;
      const method = typeof input === "string" ? "GET" : req.method;
      const body = method === "POST" ? await req.clone().json() : null;
      calls.push({ url, method, body });
      const resBody = url.includes("/interfaces") && method === "POST" ? created : { interfaces: [...seed, created] };
      return new Response(JSON.stringify(resBody), { status: 200, headers: { "Content-Type": "application/json" } });
    });

    mount(owner);
    fireEvent.click(screen.getByText("New interface"));
    fireEvent.input(await screen.findByLabelText("Name"), { target: { value: "disp-1-tcp2" } });
    fireEvent.change(screen.getByLabelText("Type"), { target: { value: "tcp" } });
    fireEvent.change(screen.getByLabelText("Component"), { target: { value: "disp-1" } });
    fireEvent.change(screen.getByLabelText("Node"), { target: { value: "edge-east" } });
    fireEvent.input(screen.getByLabelText("Target"), { target: { value: "10.0.0.2:80" } });
    fireEvent.click(screen.getByText("Create interface"));

    await waitFor(() => {
      const posts = calls.filter((c) => c.method === "POST" && c.url.includes("/interfaces"));
      expect(posts.length).toBe(1);
    });
    const post = calls.find((c) => c.method === "POST" && c.url.includes("/interfaces"))!;
    expect(post.body).toMatchObject({
      name: "disp-1-tcp2",
      type: "tcp",
      component: "disp-1",
      node: "edge-east",
      params: { target: "10.0.0.2:80" },
    });
    // The create Drawer gives way to the new interface's detail Drawer.
    await waitFor(() => expect(screen.queryByText("Create interface")).toBeNull());
  });
});
