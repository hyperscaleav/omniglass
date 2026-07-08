import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import AddReachabilityCheck from "./AddReachabilityCheck";
import { NODES_KEY } from "../lib/nodes";
import { ME_KEY, type Me } from "../lib/auth";

// The Add-check affordance authors a valid reachability check: an interface (type =
// protocol, this component, params.target) then a poll task over it. Data is seeded
// into the query cache; the two create POSTs are faked at the fetch seam so the test
// asserts the exact bodies and the two-step error path without a server.
const owner: Me = { principal: { id: "p", kind: "human" }, permissions: [">"], grants: [] };
const ifaceOnly: Me = { principal: { id: "i", kind: "human" }, permissions: ["interface:create", "interface:read", "task:read"], grants: [] };
const taskOnly: Me = { principal: { id: "t", kind: "human" }, permissions: ["task:create", "interface:read", "task:read"], grants: [] };

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function mount(me: Me, component = "disp-1") {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...NODES_KEY], [{ name: "edge-hq", enrolled: true }]);
  return render(() => (
    <QueryClientProvider client={qc}>
      <AddReachabilityCheck component={component} />
    </QueryClientProvider>
  ));
}

// A fetch seam that records every POST body and answers the two creates.
function seam(taskStatus = 201) {
  const calls: { url: string; method: string; body: unknown }[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const req = input as Request;
    const url = typeof input === "string" ? input : req.url;
    const method = typeof input === "string" ? "GET" : req.method;
    const body = method === "POST" ? await req.clone().json() : null;
    calls.push({ url, method, body });
    if (url.includes("/interfaces") && method === "POST") return json({ name: "disp-1-tcp", type: "tcp", component: "disp-1" }, 201);
    if (url.includes("/tasks") && method === "POST") {
      return taskStatus === 201 ? json({ id: "t-9", interface: "disp-1-tcp", mode: "poll", enabled: true }, 201) : json({ detail: "the interface already has a task." }, taskStatus);
    }
    // Any refetch after invalidation resolves to an empty envelope.
    return json({ interfaces: [], tasks: [], nodes: [] });
  });
  return calls;
}

describe("AddReachabilityCheck", () => {
  afterEach(() => vi.restoreAllMocks());

  it("shows the Add check button only with BOTH interface:create and task:create", () => {
    const a = mount(ifaceOnly);
    expect(screen.queryByText("Add check")).toBeNull(); // task:create missing
    a.unmount();
    const b = mount(taskOnly);
    expect(screen.queryByText("Add check")).toBeNull(); // interface:create missing
    b.unmount();
    mount(owner);
    expect(screen.getByText("Add check")).toBeTruthy();
  });

  it("opens the form and, on submit, creates the interface then the task with the right bodies", async () => {
    const calls = seam();
    mount(owner);
    fireEvent.click(screen.getByText("Add check"));
    fireEvent.input(await screen.findByLabelText("Target"), { target: { value: "10.0.0.1:5000" } });
    fireEvent.click(screen.getByText("Add check", { selector: "button[type=submit]" }));

    await waitFor(() => {
      const posts = calls.filter((c) => c.method === "POST");
      expect(posts.length).toBe(2);
    });
    const posts = calls.filter((c) => c.method === "POST");
    const ifacePost = posts.find((c) => c.url.includes("/interfaces"))!;
    const taskPost = posts.find((c) => c.url.includes("/tasks"))!;
    // The interface: type = protocol, owner = THIS component, target in params.
    expect(ifacePost.body).toMatchObject({ name: "disp-1-tcp", type: "tcp", component: "disp-1", params: { target: "10.0.0.1:5000" } });
    // The task: over the created interface, poll mode, enabled.
    expect(taskPost.body).toMatchObject({ interface: "disp-1-tcp", mode: "poll", enabled: true });
    // The interface create ran before the task create (it names the task's interface).
    expect(posts.indexOf(ifacePost)).toBeLessThan(posts.indexOf(taskPost));
  });

  it("surfaces the error when the task create fails after the interface was created (does not swallow it)", async () => {
    seam(409); // interface created (201), task create refused (409)
    mount(owner);
    fireEvent.click(screen.getByText("Add check"));
    fireEvent.input(await screen.findByLabelText("Target"), { target: { value: "10.0.0.1:5000" } });
    fireEvent.click(screen.getByText("Add check", { selector: "button[type=submit]" }));

    // The partial state is surfaced clearly: the interface exists, the task did not
    // schedule, and the operator can retry just the task.
    expect(await screen.findByText(/was created, but the task could not be scheduled/i)).toBeTruthy();
    expect(screen.getByText("Retry task")).toBeTruthy();
  });
});
