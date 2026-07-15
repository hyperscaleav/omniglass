import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Tasks from "./Tasks";
import { TASKS_KEY, type Task } from "../lib/tasks";
import { INTERFACES_KEY } from "../lib/interfaces";
import { ME_KEY, type Me } from "../lib/auth";

// The Tasks page is a config over the shared FlatList: a row per task, a row opening
// the side Drawer detail (facts + inline edit + delete), and a create Drawer. Data is
// seeded into the query cache so no server is needed.
const seed: Task[] = [
  { id: "t-tcp", interface_id: "if-tcp", mode: "poll", enabled: true, display_name: "HQ display TCP" },
  { id: "t-icmp", interface_id: "if-icmp", mode: "poll", enabled: false },
  { id: "t-sess", interface_id: "if-sess", mode: "listen", enabled: true },
];

const owner: Me = { principal: { id: "p", kind: "human" }, permissions: [">"], grants: [] };
const reader: Me = { principal: { id: "r", kind: "human" }, permissions: ["task:read"], grants: [] };

function mount(me: Me) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...TASKS_KEY], seed);
  qc.setQueryData([...INTERFACES_KEY], [
    { id: "if-tcp", name: "disp-1-tcp", type: "tcp", component: "disp-1" },
    { id: "if-icmp", name: "disp-1-icmp", type: "icmp", component: "disp-1" },
    { id: "if-sess", name: "sess-1", type: "tcp", component: "disp-1" },
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

  it("hides Edit and Delete in the detail Drawer without update/delete perms", async () => {
    mount(reader);
    fireEvent.click(screen.getByText("HQ display TCP"));
    await screen.findByRole("dialog");
    expect(screen.queryByText("Edit")).toBeNull();
    expect(screen.queryByText("Delete")).toBeNull();
  });
});
