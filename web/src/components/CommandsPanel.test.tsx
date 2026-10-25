import { describe, it, expect, vi, afterEach } from "vitest";
import { createEffect } from "solid-js";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library";
import { Router, Route, useLocation } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import CommandsPanel from "./CommandsPanel";
import Toaster from "./Toaster";
import { componentCommandsKey, type Command } from "../lib/commands";
import { ENDPOINTS_KEY, type Endpoint } from "../lib/endpoints";
import { DRIVERS_KEY, type Driver } from "../lib/drivers";
import { COMMAND_TYPES_KEY, type CommandTypeRow } from "../lib/command_types";
import { ME_KEY, type Me } from "../lib/auth";
import { clearToasts } from "../lib/toast";
import { uuidFor } from "../lib/testids";

// The commands panel (#1155): a component's commands, each where it stands
// (queued, delivered, awaiting its reply or observation, then settled, failed
// or timed out with why), a row opening in place to its arc and evidence, and
// the issue that raises a toast following the new command live. Rows are
// seeded into the query cache; the issue and the toast's read are faked.

const comp = uuidFor("cmd-bar");
const at = (secondsAgo: number) => new Date(Date.now() - secondsAgo * 1000).toISOString();
const later = (seconds: number) => new Date(Date.now() + seconds * 1000).toISOString();

function command(over: Partial<Command>): Command {
  return {
    id: 1,
    component: "videobar-1",
    component_id: comp,
    command_type: "set-volume",
    intended: 42 as unknown as Command["intended"],
    target_kind: "metric",
    target: "volume",
    status: "issued",
    phase: "queued",
    settle_mode: "reply",
    settle_window_seconds: 15,
    deadline: later(12),
    issued_at: at(3),
    endpoint: "wss",
    endpoint_id: uuidFor("cmd-ep"),
    node: "edge-hq",
    driver: "kestrel-ws",
    issuer: "dev",
    issuer_id: uuidFor("cmd-dev"),
    ...over,
  } as Command;
}

const rows: Command[] = [
  command({ id: 4, phase: "awaiting-reply", delivered_at: at(2) }),
  command({
    id: 3,
    status: "settled",
    phase: "settled",
    settled_by: "reply",
    reason: "the reply carried 40, the intended value",
    reply: '{"id":"a1","type":"result","value":40}',
    intended: 40 as unknown as Command["intended"],
    delivered_at: at(60),
    executed_at: at(59),
    settled_at: at(59),
    issued_at: at(61),
  }),
  command({
    id: 2,
    command_type: "set-input",
    target_kind: "property",
    target: "video-input",
    intended: "hdmi3" as unknown as Command["intended"],
    status: "settled",
    phase: "settled",
    settle_mode: "observation",
    settled_by: "observation",
    reason: "observed hdmi3, the intended value",
    observed: { value: "hdmi3" as unknown as Command["intended"], at: at(100) },
    endpoint: undefined,
    node: undefined,
    driver: undefined,
    issued_at: at(110),
    settled_at: at(100),
  }),
  command({ id: 1, status: "timed-out", phase: "timed-out", settled_by: "window", reason: "queued for node edge-dark, which did not take it within its 15s settle window", issued_at: at(300), settled_at: at(285) }),
];

const operator: Me = { principal: { id: "op", kind: "human" }, permissions: ["*:read", "command:issue"], grants: [] };
const viewer: Me = { principal: { id: "vw", kind: "human" }, permissions: ["*:read"], grants: [] };

const endpoints = [{ id: uuidFor("cmd-ep"), name: "wss", transport: "wss", component_id: comp, driver: "kestrel-ws" }] as Endpoint[];
const drivers = [
  { id: uuidFor("cmd-drv"), name: "kestrel-ws", label: "Kestrel Control Socket", official: true, spec: { version: 1, transport: "wss", commands: [{ command_type: "set-volume", request: {} }] } },
] as Driver[];
const catalog: CommandTypeRow[] = [{ name: "set-volume", label: "Set Volume", settle_window_seconds: 15, target_metric_type: "volume", official: true }];

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

let where = "";
function Where() {
  const loc = useLocation();
  createEffect(() => {
    where = `${loc.pathname}${loc.search}`;
  });
  return null;
}

function mount(opts: { me?: Me; rows?: Command[]; open?: number; endpoints?: Endpoint[] } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], opts.me ?? operator);
  if (opts.rows !== undefined || !("rows" in opts)) qc.setQueryData([...componentCommandsKey(comp)], opts.rows ?? rows);
  qc.setQueryData([...ENDPOINTS_KEY], opts.endpoints ?? endpoints);
  qc.setQueryData([...DRIVERS_KEY], drivers);
  qc.setQueryData([...COMMAND_TYPES_KEY], catalog);
  window.history.pushState({}, "", `/web/components/${comp}?tab=activity`);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router base="/web" root={(p) => <>{p.children}<Where /><Toaster /></>}>
        <Route path="/components/:id" component={() => <CommandsPanel component={comp} name="videobar-1" componentId={comp} open={opts.open} />} />
      </Router>
    </QueryClientProvider>
  ));
}

afterEach(() => {
  cleanup();
  clearToasts();
  vi.restoreAllMocks();
});

describe("CommandsPanel", () => {
  it("lists the history newest first, each command where it stands", () => {
    mount();
    const list = screen.getByRole("list", { name: "Commands issued to videobar-1" });
    const items = within(list).getAllByRole("listitem").filter((li) => li.dataset.testid?.startsWith("command-"));
    expect(items.map((li) => li.dataset.testid)).toEqual(["command-4", "command-3", "command-2", "command-1"]);
    expect(within(items[0]).getByTestId("command-phase").textContent).toBe("awaiting reply");
    expect(items[0].textContent).toMatch(/window closes in 1\ds/);
    expect(within(items[1]).getByTestId("command-phase").textContent).toBe("settled");
    expect(items[1].textContent).toContain("the reply carried 40, the intended value");
    expect(within(items[3]).getByTestId("command-phase").textContent).toBe("timed out");
    expect(items[3].textContent).toContain("edge-dark");
    expect(screen.getByTestId("commands-in-flight").textContent).toBe("1 in flight");
  });

  it("opens a command in place to its arc, its reply and what carried it", async () => {
    mount();
    const row = screen.getByTestId("command-3");
    const toggle = within(row).getByRole("button", { name: "Details of set-volume command 3" });
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(toggle);
    const detail = within(row).getByTestId("command-detail");
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    expect(toggle.getAttribute("aria-controls")).toBe(detail.id);
    const steps = within(detail).getByRole("list", { name: "The command's transitions" });
    const items = within(steps).getAllByRole("listitem");
    expect(items.map((li) => li.dataset.step)).toEqual(["issued", "delivered", "executed", "terminal"]);
    expect(items[1].textContent).toContain("Delivered to edge-hq");
    expect(items[2].textContent).toContain("Answered");
    expect(items[3].textContent).toContain("Settled");
    expect(within(detail).getByTestId("command-reply").textContent).toBe('{"id":"a1","type":"result","value":40}');
    expect(detail.textContent).toContain("kestrel-ws");
    expect(detail.textContent).toContain("Issued by");
    expect(detail.textContent).toContain("dev");
  });

  it("shows an observation-settled command's sample, with no wire it never rode", () => {
    mount();
    fireEvent.click(within(screen.getByTestId("command-2")).getByRole("button", { name: /Details of set-input/ }));
    const detail = within(screen.getByTestId("command-2")).getByTestId("command-detail");
    expect(detail.textContent).toContain("Observed");
    expect(detail.textContent).toContain("hdmi3");
    expect(detail.textContent).not.toContain("Delivered");
    expect(within(detail).queryByTestId("command-reply")).toBeNull();
  });

  it("opens the command it is asked for", async () => {
    mount({ open: 1 });
    await waitFor(() => expect(within(screen.getByTestId("command-1")).getByTestId("command-detail")).toBeTruthy());
  });

  it("teaches an empty history, and why nothing can be issued where no driver binds a command", () => {
    mount({ rows: [] });
    expect(screen.getByTestId("commands-empty").textContent).toBe("No commands issued to this component yet.");
    cleanup();
    mount({ rows: [], endpoints: [] });
    expect(screen.getByTestId("commands-empty").textContent).toMatch(/none can reach it: no driver attached to its endpoints binds a command/);
    expect(screen.queryByRole("button", { name: "Issue a command" })).toBeNull();
  });

  it("alerts a failed read and retries it", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async () => json({ title: "Internal Server Error", status: 500, detail: "read commands" }, 500));
    mount({ rows: undefined });
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("read commands");
    const calls = fetchSpy.mock.calls.length;
    fireEvent.click(within(alert).getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(fetchSpy.mock.calls.length).toBeGreaterThan(calls));
  });

  it("offers the issue only to a caller who may issue, on a component a driver can reach", () => {
    mount({ me: viewer });
    expect(screen.queryByRole("button", { name: "Issue a command" })).toBeNull();
    cleanup();
    mount();
    expect(screen.getByRole("button", { name: "Issue a command" })).toBeTruthy();
  });

  it("issues a command, and its toast follows it live and opens its row", async () => {
    const issued = command({ id: 9, phase: "queued" });
    const settled = command({ id: 9, status: "settled", phase: "settled", settled_by: "reply", reason: "the reply carried 42, the intended value", settled_at: at(0) });
    const posted: unknown[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST" && req.url.endsWith(`/components/${comp}/commands:issue`)) {
        posted.push(await req.json());
        return json(issued);
      }
      if (req.url.includes(`/components/${comp}/commands/9`)) return json(settled);
      if (req.url.includes(`/components/${comp}/commands`)) return json({ component: "videobar-1", commands: [issued, ...rows] });
      throw new Error(`unexpected ${req.method} ${req.url}`);
    });
    mount();
    fireEvent.click(screen.getByRole("button", { name: "Issue a command" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("heading", { name: "Issue a command to videobar-1" })).toBeTruthy();
    const submit = within(dialog).getByRole("button", { name: "Issue" });
    expect((submit as HTMLButtonElement).disabled).toBe(true);
    fireEvent.input(within(dialog).getByLabelText("Value"), { target: { value: "loud" } });
    expect((submit as HTMLButtonElement).disabled).toBe(true);
    fireEvent.input(within(dialog).getByLabelText("Value"), { target: { value: "42" } });
    expect((submit as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(submit);
    await waitFor(() => expect(posted).toEqual([{ command_type: "set-volume", value: 42 }]));
    const toast = await screen.findByTestId("toast");
    expect(toast.textContent).toContain("set-volume issued to videobar-1");
    await waitFor(() => expect(within(toast).getByTestId("command-toast").textContent).toContain("settled"));
    expect(within(toast).getByTestId("command-toast").textContent).toContain("the reply carried 42");
    fireEvent.click(within(toast).getByRole("button", { name: "Open" }));
    await waitFor(() => expect(where).toBe(`/web/components/${comp}?tab=activity&command=9`));
    expect(screen.queryByTestId("toast")).toBeNull();
  });
});
