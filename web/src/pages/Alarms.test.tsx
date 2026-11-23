import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor, within } from "@solidjs/testing-library";
import { Router, Route, useLocation } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Alarms from "./Alarms";
import { ME_KEY, type Me } from "../lib/auth";
import { severityLevelsKey } from "../lib/severity";
import { uuidFor } from "../lib/testids";

// The alarm queue (#2333): the fleet's active alarms as one list. The network is
// the seam we fake (fetch), so the page is driven as an operator drives it.
const alarm = (over: Record<string, unknown>) => ({
  id: uuidFor(String(over.message)),
  component: "cam-1",
  component_id: uuidFor("cam-1"),
  severity: "critical",
  message: "no signal",
  dedup_key: "no-signal",
  raised_at: new Date(Date.now() - 5 * 60_000).toISOString(),
  active: true,
  acknowledged: false,
  location: "room-1",
  location_path: "hq.room-1",
  systems: ["boardroom"],
  ...over,
});
const seed = [
  alarm({ message: "no signal" }),
  alarm({ message: "fan slow", component: "dsp-1", severity: "warning", location: "room-2", location_path: "hq.room-2", systems: [] }),
];
const me = (permissions: string[]): Me => ({ user: { id: "p", kind: "person" }, person: { username: "ada" }, permissions, grants: [] });
const OPERATOR = ["component:read", "alarm:acknowledge", "alarm:snooze", "assistant:use"];

let calls: { method: string; url: URL }[] = [];

function mount(who: Me, at = "/") {
  calls = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
    const req = input as Request;
    const url = new URL(typeof input === "string" ? input : req.url, "http://local");
    const method = (init?.method ?? req.method ?? "GET").toUpperCase();
    calls.push({ method, url });
    if (url.pathname.endsWith(":acknowledge")) return new Response(JSON.stringify(alarm({ message: "no signal", acknowledged: true })), { status: 200, headers: { "Content-Type": "application/json" } });
    return new Response(JSON.stringify({ alarms: seed }), { status: 200, headers: { "Content-Type": "application/json" } });
  });
  window.history.pushState({}, "", at);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData([...ME_KEY], who);
  qc.setQueryData([...severityLevelsKey], []);
  let search = () => "";
  const Probe = () => {
    const loc = useLocation();
    search = () => loc.search;
    return <Alarms />;
  };
  render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={Probe} />
      </Router>
    </QueryClientProvider>
  ));
  return { search: () => search() };
}
const listReads = () => calls.filter((c) => c.method === "GET" && c.url.pathname.endsWith("/alarms"));

afterEach(() => {
  vi.restoreAllMocks();
  window.history.pushState({}, "", "/");
});

describe("Alarms page", () => {
  it("renders each active alarm with its component, system, location and state", async () => {
    mount(me(OPERATOR));
    expect(await screen.findByText("no signal")).toBeInTheDocument();
    expect(screen.getByText("fan slow")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "cam-1" })).toHaveAttribute("href", expect.stringContaining(`/components/${uuidFor("cam-1")}`));
    expect(screen.getByText("boardroom")).toBeInTheDocument();
    expect(screen.getByText("room-2")).toBeInTheDocument();
    expect(screen.getAllByText("new").length).toBe(2);
  });

  it("reads its filter from the address and asks the server for exactly that", async () => {
    mount(me(OPERATOR), "/?state=active&severity=critical,high&ack=no&location=hq");
    await screen.findByText("no signal");
    const q = listReads()[0].url.searchParams;
    // Each level is its own parameter: the server reads occurrences, not a comma list.
    expect(q.getAll("severity")).toEqual(["critical", "high"]);
    expect(q.get("acknowledged")).toBe("false");
    expect(q.get("location")).toBe("hq");
  });

  it("writes the filter back to the address when a chip goes", async () => {
    const { search } = mount(me(OPERATOR), "/?state=active&severity=critical&ack=no");
    await screen.findByText("no signal");
    fireEvent.click(screen.getByRole("button", { name: /clear/i }));
    await waitFor(() => expect(search()).not.toContain("severity"));
    expect(search()).not.toContain("ack=");
    expect(search()).toContain("state=active");
  });

  it("offers a viewer the state of each alarm and no actions", async () => {
    mount(me(["component:read"]));
    await screen.findByText("no signal");
    expect(screen.queryByRole("button", { name: /acknowledge/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /snooze/i })).toBeNull();
    expect(screen.queryByTestId("alarm-actions")).toBeNull();
  });

  it("acknowledges from the row and reads the queue again", async () => {
    mount(me(OPERATOR));
    await screen.findByText("no signal");
    const before = listReads().length;
    fireEvent.click(screen.getAllByRole("button", { name: "Acknowledge" })[0]);
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.url.pathname.endsWith(":acknowledge"))).toBe(true));
    await waitFor(() => expect(listReads().length).toBeGreaterThan(before));
  });

  // #2441: a name is not an address where it is not unique. Two components both
  // named panel-1: every action and the history read must hit the row's uuid.
  describe("two components sharing a name", () => {
    const twins = [
      alarm({ message: "link down", component: "panel-1", component_id: uuidFor("panel-1:a") }),
      alarm({ message: "link flap", component: "panel-1", component_id: uuidFor("panel-1:b") }),
    ];
    const mountTwins = (fail = false) => {
      calls = [];
      vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
        const req = input as Request;
        const url = new URL(typeof input === "string" ? input : req.url, "http://local");
        const method = (init?.method ?? req.method ?? "GET").toUpperCase();
        calls.push({ method, url });
        const json = (b: unknown, status = 200) => new Response(JSON.stringify(b), { status, headers: { "Content-Type": "application/json" } });
        // The bare name addresses nothing: it is ambiguous, so the server says 404.
        if (url.pathname.includes("/components/panel-1/")) return json({ title: "Not Found", detail: "no such component: " + "x".repeat(300) }, 404);
        if (fail && method === "POST") return json({ title: "Bad", detail: "boom ".repeat(80) }, 500);
        if (url.pathname.endsWith(":timeline")) return json({ component: "panel-1", alarm_id: "x", entries: [] });
        if (method === "POST") return json({ ...twins[1], acknowledged: true });
        return json({ alarms: twins });
      });
      window.history.pushState({}, "", "/");
      const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
      qc.setQueryData([...ME_KEY], me(OPERATOR));
      qc.setQueryData([...severityLevelsKey], []);
      render(() => (
        <QueryClientProvider client={qc}>
          <Router><Route path="*" component={Alarms} /></Router>
        </QueryClientProvider>
      ));
    };

    it("acknowledges and snoozes by the row's uuid", async () => {
      mountTwins();
      await screen.findByText("link flap");
      fireEvent.click(screen.getAllByRole("button", { name: "Acknowledge" })[1]);
      await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.url.pathname.includes(`/components/${uuidFor("panel-1:b")}/alarms/`) && c.url.pathname.endsWith(":acknowledge"))).toBe(true));
      fireEvent.click(screen.getAllByRole("button", { name: /^Snooze/ })[1]);
      fireEvent.click(await screen.findByRole("button", { name: "15 minutes" }));
      await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.url.pathname.includes(`/components/${uuidFor("panel-1:b")}/alarms/`) && c.url.pathname.endsWith(":snooze"))).toBe(true));
      expect(calls.some((c) => c.url.pathname.includes("/components/panel-1/"))).toBe(false);
    });

    it("reads the drawer's history by the row's uuid", async () => {
      mountTwins();
      fireEvent.click(await screen.findByText("link flap"));
      await waitFor(() => expect(calls.some((c) => c.url.pathname.includes(`/components/${uuidFor("panel-1:b")}/alarms/`) && c.url.pathname.endsWith(":timeline"))).toBe(true));
      expect(calls.some((c) => c.url.pathname.includes("/components/panel-1/"))).toBe(false);
    });

    it("shows a failed action as one short line with Retry, never the raw detail", async () => {
      mountTwins(true);
      await screen.findByText("link flap");
      fireEvent.click(screen.getAllByRole("button", { name: "Acknowledge" })[0]);
      const alert = await screen.findByRole("alert");
      expect(alert.textContent).not.toContain("boom boom");
      expect(alert.textContent!.length).toBeLessThan(80);
      expect(within(alert).getByRole("button", { name: "Retry" })).toBeInTheDocument();
    });

    it("shows a failed history read with Retry, not an endless spinner", async () => {
      mountTwins();
      calls = [];
      const f = vi.mocked(globalThis.fetch);
      const base = f.getMockImplementation()!;
      f.mockImplementation(async (i, init) => {
        const u = new URL(typeof i === "string" ? i : (i as Request).url, "http://local");
        if (u.pathname.endsWith(":timeline")) return new Response(JSON.stringify({ detail: "down" }), { status: 500, headers: { "Content-Type": "application/json" } });
        return base(i, init);
      });
      fireEvent.click(await screen.findByText("link flap"));
      expect(await screen.findByRole("button", { name: "Retry" })).toBeInTheDocument();
      expect(screen.queryByText("Reading the timeline…")).toBeNull();
    });
  });
});

// #2426: the history. A state filter held in the address, Raised, Cleared and
// Duration columns, a cleared row opening the same drawer, and the lifecycle
// walk's cell fixes (a system-placed location, links, who acknowledged).
describe("Alarms page history (#2426)", () => {
  const T0 = Date.now() - 3 * 3_600_000;
  const open = alarm({ message: "still dark", raised_at: new Date(T0).toISOString(), system_ids: [uuidFor("boardroom")], location: "room-1", location_path: "hq.room-1", location_id: uuidFor("room-1") });
  const gone = alarm({
    message: "was dark",
    component: "dsp-1",
    component_id: uuidFor("dsp-1"),
    raised_at: new Date(T0).toISOString(),
    cleared_at: new Date(T0 + 12 * 60_000).toISOString(),
    active: false,
    acknowledged: true,
    acknowledged_by: "ada",
    systems: [],
  });
  const mountState = (at: string) => {
    calls = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input, init) => {
      const req = input as Request;
      const url = new URL(typeof input === "string" ? input : req.url, "http://local");
      calls.push({ method: (init?.method ?? req.method ?? "GET").toUpperCase(), url });
      const json = (b: unknown) => new Response(JSON.stringify(b), { status: 200, headers: { "Content-Type": "application/json" } });
      if (url.pathname.endsWith(":timeline")) return json({ component: "dsp-1", alarm_id: "x", entries: [] });
      const state = url.searchParams.get("state") ?? "active";
      return json({ alarms: state === "cleared" ? [gone] : state === "all" ? [open, gone] : [open] });
    });
    window.history.pushState({}, "", at);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    qc.setQueryData([...ME_KEY], me(OPERATOR));
    qc.setQueryData([...severityLevelsKey], []);
    let search = () => "";
    const Probe = () => {
      const loc = useLocation();
      search = () => loc.search;
      return <Alarms />;
    };
    render(() => (
      <QueryClientProvider client={qc}>
        <Router><Route path="*" component={Probe} /></Router>
      </QueryClientProvider>
    ));
    return { search: () => search() };
  };

  it("holds the state in the address and reads it from the server", async () => {
    const { search } = mountState("/");
    await screen.findByText("still dark");
    expect(listReads()[0].url.searchParams.get("state")).toBeNull();
    const group = screen.getByRole("radiogroup", { name: "Alarm state" });
    fireEvent.click(within(group).getByLabelText("Cleared"));
    expect(await screen.findByText("was dark")).toBeInTheDocument();
    expect(search()).toContain("state=cleared");
    expect(listReads().at(-1)!.url.searchParams.get("state")).toBe("cleared");
    expect(screen.queryByText("still dark")).toBeNull();
  });

  it("opens on the state the address names", async () => {
    mountState("/?state=all");
    await screen.findByText("was dark");
    expect(screen.getByText("still dark")).toBeInTheDocument();
    expect(within(screen.getByRole("radiogroup", { name: "Alarm state" })).getByLabelText("All")).toBeChecked();
  });

  it("shows when a cleared alarm cleared and how long it lasted", async () => {
    mountState("/?state=cleared");
    const row = (await screen.findByText("was dark")).closest("tr")!;
    expect(within(row).getByTestId("alarm-duration")).toHaveTextContent("12m");
    expect(within(row).getByTestId("alarm-cleared")).toHaveTextContent(/ago/);
    expect(screen.getByRole("columnheader", { name: /Duration/ })).toBeInTheDocument();
    expect(screen.getByRole("columnheader", { name: /Cleared/ })).toBeInTheDocument();
  });

  it("ticks an open alarm's duration while it stays open", async () => {
    mountState("/");
    const row = (await screen.findByText("still dark")).closest("tr")!;
    expect(within(row).getByTestId("alarm-duration")).toHaveTextContent("3h");
    expect(within(row).getByTestId("alarm-duration").getAttribute("data-open")).toBe("true");
  });

  it("opens a cleared row in the same drawer, with its history and no clear actions", async () => {
    mountState("/?state=cleared");
    fireEvent.click(await screen.findByText("was dark"));
    await waitFor(() => expect(calls.some((c) => c.url.pathname.includes(`/components/${uuidFor("dsp-1")}/alarms/`) && c.url.pathname.endsWith(":timeline"))).toBe(true));
    const drawer = await screen.findByRole("dialog");
    expect(within(drawer).getByText("Lasted")).toBeInTheDocument();
    expect(within(drawer).getByText("12m")).toBeInTheDocument();
    expect(within(drawer).getByText("Cleared")).toBeInTheDocument();
  });

  it("links the system and the location, and names who acknowledged", async () => {
    mountState("/?state=all");
    await screen.findByText("still dark");
    expect(screen.getByRole("link", { name: "boardroom" })).toHaveAttribute("href", `/systems/${uuidFor("boardroom")}`);
    expect(screen.getAllByRole("link", { name: "room-1" })[0]).toHaveAttribute("href", `/locations/${uuidFor("room-1")}`);
    expect(screen.getByText("acked by ada")).toBeInTheDocument();
  });
});
