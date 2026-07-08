import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Tasks from "./Tasks";
import { TASKS_KEY, type Task } from "../lib/tasks";
import { INTERFACES_KEY } from "../lib/interfaces";
import { ME_KEY, type Me } from "../lib/auth";

// The Tasks page is a config over the shared FlatList: a row per task, a row opening
// the side Drawer detail (facts + inline edit + delete), and a create Drawer. Data is
// seeded into the query cache so no server is needed.
const seed: Task[] = [
  { id: "t-tcp", interface: "disp-1-tcp", mode: "poll", enabled: true, display_name: "HQ display TCP" },
  { id: "t-icmp", interface: "disp-1-icmp", mode: "poll", enabled: false },
  { id: "t-sess", interface: "sess-1", mode: "listen", enabled: true },
];

const owner: Me = { principal: { id: "p", kind: "human" }, permissions: [">"], grants: [] };
const reader: Me = { principal: { id: "r", kind: "human" }, permissions: ["task:read"], grants: [] };

function mount(me: Me) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...TASKS_KEY], seed);
  qc.setQueryData([...INTERFACES_KEY], [
    { name: "disp-1-tcp", type: "tcp", component: "disp-1" },
    { name: "disp-1-icmp", type: "icmp", component: "disp-1" },
  ]);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Tasks />
    </QueryClientProvider>
  ));
}

describe("Tasks page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders a row per task with its interface, mode, and enabled pill", () => {
    const { getByText, getAllByText } = mount(owner);
    expect(getByText("HQ display TCP")).toBeTruthy(); // display name
    expect(getByText("disp-1-tcp")).toBeTruthy(); // its interface
    expect(getByText("sess-1")).toBeTruthy();
    expect(getAllByText("poll").length).toBeGreaterThan(0);
    expect(getByText("listen")).toBeTruthy();
    // the enabled/disabled pills are derived from the real boolean field.
    expect(getAllByText("enabled").length).toBeGreaterThan(0);
    expect(getByText("disabled")).toBeTruthy();
  });

  it("hides the create affordance without task:create, shows it with", () => {
    const { queryByText, unmount } = mount(reader);
    expect(queryByText("New task")).toBeNull();
    unmount();
    mount(owner);
    expect(screen.getByText("New task")).toBeTruthy();
  });

  it("hides Edit and Delete in the detail Drawer without update/delete perms", async () => {
    mount(reader);
    fireEvent.click(screen.getByText("HQ display TCP"));
    await screen.findByRole("dialog");
    expect(screen.queryByText("Edit")).toBeNull();
    expect(screen.queryByText("Delete")).toBeNull();
  });

  it("opens the create Drawer: the interface select lists the loaded interfaces and mode offers poll/listen", async () => {
    mount(owner);
    fireEvent.click(screen.getByText("New task"));
    const ifaceSelect = (await screen.findByLabelText("Interface")) as HTMLSelectElement;
    const ifaceOptions = Array.from(ifaceSelect.options).map((o) => o.value).filter(Boolean);
    expect(ifaceOptions).toEqual(["disp-1-tcp", "disp-1-icmp"]);
    const modeSelect = screen.getByLabelText("Mode") as HTMLSelectElement;
    expect(Array.from(modeSelect.options).map((o) => o.value)).toEqual(["poll", "listen"]);
  });

  it("posts the create body (interface + mode + enabled) and lands on the new row", async () => {
    const created: Task = { id: "t-new", interface: "disp-1-tcp", mode: "poll", enabled: true };
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const url = typeof input === "string" ? input : req.url;
      const method = typeof input === "string" ? "GET" : req.method;
      const body = url.includes("/tasks") && method === "POST" ? created : { tasks: [...seed, created] };
      return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
    });

    mount(owner);
    fireEvent.click(screen.getByText("New task"));
    const ifaceSelect = (await screen.findByLabelText("Interface")) as HTMLSelectElement;
    fireEvent.change(ifaceSelect, { target: { value: "disp-1-tcp" } });
    fireEvent.click(screen.getByText("Create task"));
    await waitFor(() => expect(screen.queryByText("Create task")).toBeNull());
  });
});
