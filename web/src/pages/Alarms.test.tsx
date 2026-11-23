import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@solidjs/testing-library";
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
    mount(me(OPERATOR), "/?state=active&severity=critical&ack=no&location=hq");
    await screen.findByText("no signal");
    const q = listReads()[0].url.searchParams;
    expect(q.getAll("severity")).toEqual(["critical"]);
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
});
