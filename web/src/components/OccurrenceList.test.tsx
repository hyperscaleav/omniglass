import { tipOf } from "../test-tip";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, within, waitFor, cleanup } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import OccurrenceList from "./OccurrenceList";
import OccurrenceRow from "./OccurrenceRow";
import type { Occurrence } from "../lib/occurrence";
import { EVENTS_KEY, type ComponentEvents } from "../lib/events";
import { LOGS_KEY, OUTPOST_LOGS_KEY, type ComponentLogs, type OutpostLogs } from "../lib/logs";

// The one list and row every event or log line renders through (#2437). Data is
// seeded into the query cache so no server is needed, except where a test asserts
// the request a filter sends.
const nowIso = new Date().toISOString();
const EVENT_HINT = "events hint";
const LOG_HINT = "logs hint";
const TASK = "9f2c4e7a1b3d5f60".repeat(4);

afterEach(cleanup);

function client() {
  return new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
}

function mountList(qc: QueryClient, ui: () => any) {
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router><Route path="*" component={ui} /></Router>
    </QueryClientProvider>
  ));
}

const events: ComponentEvents = {
  component: "disp-1",
  events: [
    { ts: nowIso, key: "call-started", event_type_id: "0192a5f0-1111-7000-8000-0000000000a1", origin: "caught", message: "call started with room-204", provenance: "observed", source: "xapi", attributes: { peer: "sip:room-204", protocol: "sip" } },
    { ts: nowIso, key: "meeting.ended", event_type_id: "0192a5f0-2222-7000-8000-0000000000a2", origin: "derived", message: "meeting ended (call dropped, no rejoin)", provenance: "observed", source: "rule" },
  ],
};

function mountEvents(data: ComponentEvents = events) {
  const qc = client();
  qc.setQueryData([...EVENTS_KEY(data.component)], data);
  return mountList(qc, () => <OccurrenceList kind="events" scope={{ type: "component", name: data.component }} hint={EVENT_HINT} />);
}

function mountLogs(data: ComponentLogs) {
  const qc = client();
  qc.setQueryData([...LOGS_KEY(data.component)], data);
  return mountList(qc, () => <OccurrenceList kind="logs" scope={{ type: "component", name: data.component }} hint={LOG_HINT} />);
}

describe("OccurrenceList events", () => {
  it("renders one row per event with its key badge, message and count", () => {
    const { getByText, getAllByTestId } = mountEvents();
    expect(getByText("2 in the last 24h")).toBeTruthy();
    expect(getAllByTestId("occurrence-row")).toHaveLength(2);
    expect(getByText("call-started")).toBeTruthy();
    expect(getByText("call started with room-204")).toBeTruthy();
    expect(getByText("meeting.ended")).toBeTruthy();
  });

  it("opens a row's detail in place on click, with its attributes, and closes it again", () => {
    const { getAllByTestId, queryByText, getByText, queryByTestId } = mountEvents();
    expect(queryByTestId("occurrence-detail")).toBeNull();
    expect(queryByText(/protocol/)).toBeNull();
    fireEvent.click(within(getAllByTestId("occurrence-row")[0]).getByRole("button", { name: /call-started/ }));
    expect(getByText(/"protocol": "sip"/)).toBeTruthy();
    expect(queryByTestId("occurrence-detail")).toBeTruthy();
    fireEvent.click(within(getAllByTestId("occurrence-row")[0]).getByRole("button", { name: /call-started/ }));
    expect(queryByTestId("occurrence-detail")).toBeNull();
  });

  it("shows the empty state when a component has no events", () => {
    expect(mountEvents({ component: "c2", events: [] }).getByText(/no events in the last 24 hours/i)).toBeTruthy();
  });

  it("names the rule that derived an event, linking to it, and opens the alarm it carries in place", () => {
    const { getByText, getByRole } = mountEvents({
      component: "disp-1",
      events: [{
        ts: nowIso, key: "rule-fired", event_type_id: "0192a5f0-3333-7000-8000-0000000000a3", origin: "derived", message: "Reachability lost", provenance: "observed",
        source: "rule", instance: "tcp", derived_by_rule: "reachability-lost", derived_by_rule_id: "0192a5f0-5555-7000-8000-0000000000a5", alarm_id: "0192a5f0-4444-7000-8000-0000000000a4",
      }],
    });
    const link = getByText("reachability-lost").closest("a")!;
    // By the rule's uuid: a rename since has moved the name shown (#977).
    expect(link.getAttribute("href")).toBe("/event-rules?open=0192a5f0-5555-7000-8000-0000000000a5");
    expect(getByRole("button", { name: /^Timeline of / })).toBeTruthy();
  });

  it("names a deleted rule as recorded, as plain text that says it is gone, never as a dead link", async () => {
    const { getByText } = mountEvents({
      component: "disp-1",
      events: [{ ts: nowIso, key: "rule-cleared", event_type_id: "x", origin: "derived", message: "Old watch", provenance: "observed", source: "rule", derived_by_rule: "old-watch" }],
    });
    const name = getByText("old-watch");
    expect(name.closest("a")).toBeNull();
    expect(await tipOf(name.parentElement)).toMatch(/deleted/);
  });
});

describe("OccurrenceRow", () => {
  it("clamps a 2,000 character unbroken message to one line and wraps it in the detail", () => {
    const long = "x".repeat(2000);
    const { getByTestId, getAllByRole } = mountEvents({ component: "disp-1", events: [{ ts: nowIso, key: "k", event_type_id: "x", origin: "caught", message: long, provenance: "observed" }] });
    const msg = getByTestId("occurrence-message");
    expect(msg.classList.contains("truncate")).toBe(true);
    expect(msg.parentElement!.classList.contains("min-w-0")).toBe(true);
    fireEvent.click(getAllByRole("button", { name: /xxxx/ })[0]);
    const detail = within(getByTestId("occurrence-detail")).getByText(long);
    expect(detail.className).toMatch(/whitespace-pre-wrap/);
    expect(detail.className).toMatch(/overflow-wrap:anywhere/);
  });

  it("never shows a 64-hex id in a row: the collection-failed fixture reads as a short labelled reference", async () => {
    const { getByTestId, getAllByTestId, container } = mountEvents({
      component: "disp-1",
      events: [{ ts: nowIso, key: "collection-failed", event_type_id: "x", origin: "caught", message: `collection failed for task ${TASK}: connection refused`, provenance: "observed" }],
    });
    const row = getAllByTestId("occurrence-row")[0];
    expect(row.textContent).not.toContain(TASK);
    expect(within(row).getByTestId("message-ref").textContent).toBe(`task ${TASK.slice(0, 8)}`);
    expect(await tipOf(within(row).getByTestId("message-ref"))).toBe(TASK);
    expect(container.textContent).not.toMatch(/[0-9a-f]{64}/);
    expect(getByTestId("occurrence-message").textContent).toContain("connection refused");
  });

  it("shows the relative time with the absolute on hover", () => {
    const { getAllByTestId } = mountEvents();
    const t = within(getAllByTestId("occurrence-row")[0]).getByTestId("occurrence-time");
    fireEvent.focus(t.firstElementChild ?? t);
    expect(t.textContent).toMatch(/ago|\ds|now/);
  });
});

describe("OccurrenceList logs", () => {
  const seed: ComponentLogs = {
    component: "disp-1",
    logs: [
      { ts: nowIso, source: "syslog", severity: "err", facility: "daemon", message: "codec unreachable", attributes: { code: 504 } },
      { ts: nowIso, source: "syslog", severity: "info", facility: "kern", message: "hdmi link state changed to up" },
    ],
  };

  it("renders one row per line, the severity badge colored by level", () => {
    const { getByText, getAllByTestId } = mountLogs(seed);
    expect(getAllByTestId("occurrence-row")).toHaveLength(2);
    expect(getByText("err").classList.contains("badge-error")).toBe(true);
    expect(getByText("hdmi link state changed to up")).toBeTruthy();
  });

  it("opens the attributes, labels, source and facility in the detail", () => {
    const { getAllByTestId, getByText } = mountLogs({
      component: "disp-1",
      logs: [{ ts: nowIso, source: "syslog", facility: "daemon", message: "both", attributes: { code: 1 }, labels: { class: "kernel" }, correlation_id: "corr-9" }],
    });
    fireEvent.click(within(getAllByTestId("occurrence-row")[0]).getAllByRole("button")[0]);
    expect(getByText(/"code": 1/)).toBeTruthy();
    expect(getByText(/"class": "kernel"/)).toBeTruthy();
    expect(getByText("corr-9")).toBeTruthy();
    expect(within(getAllByTestId("occurrence-detail")[0]).getByText("daemon")).toBeTruthy();
  });

  it("says when a line arrived apart from the time it claims", () => {
    const own = new Date(Date.now() - 3 * 3600_000).toISOString();
    const { getAllByTestId, getByText } = mountLogs({ component: "disp-1", logs: [{ ts: own, received_at: nowIso, message: "booted before the outpost read it" }] });
    fireEvent.click(within(getAllByTestId("occurrence-row")[0]).getAllByRole("button")[0]);
    expect(getByText("Received")).toBeTruthy();
  });

  it("shows the empty state when a component has no logs", () => {
    expect(mountLogs({ component: "c2", logs: [] }).getByText(/no log lines in the last 24 hours/i)).toBeTruthy();
  });

  it("serves an outpost's self-logs with its own title and offers no filter", () => {
    const outpost: OutpostLogs = { outpost: "site-a", logs: [{ ts: nowIso, source: "outpost", severity: "info", facility: "collection", message: "worklist pulled" }] };
    const qc = client();
    qc.setQueryData([...OUTPOST_LOGS_KEY("site-a")], outpost);
    const { getByText, queryByLabelText } = mountList(qc, () => <OccurrenceList kind="logs" scope={{ type: "outpost", name: "site-a" }} title="Self-logs" hint={LOG_HINT} />);
    expect(getByText("Self-logs")).toBeTruthy();
    expect(getByText("worklist pulled")).toBeTruthy();
    expect(queryByLabelText("Severity at least")).toBeNull();
    expect(queryByLabelText("Message contains")).toBeNull();
  });
});

// Each filter changes the request: the read narrows server-side, so the request
// is what proves it.
describe("OccurrenceList filters", () => {
  function captureRequests() {
    const urls: string[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input: RequestInfo | URL) => {
      const u = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
      urls.push(u);
      const body = u.includes("/events") ? { component: "p-1", events: [] } : { component: "p-1", logs: [], sources: ["nvp", "tcp"] };
      return new Response(JSON.stringify(body), { status: 200, headers: { "content-type": "application/json" } });
    });
    return urls;
  }
  afterEach(() => vi.restoreAllMocks());
  const last = (urls: string[]) => new URL(urls[urls.length - 1], "http://x");

  it("events: key, source and text each narrow the read", async () => {
    const urls = captureRequests();
    const { getByLabelText } = mountList(new QueryClient({ defaultOptions: { queries: { retry: false } } }), () => <OccurrenceList kind="events" scope={{ type: "component", name: "p-1" }} hint={EVENT_HINT} />);
    await waitFor(() => expect(urls.length).toBeGreaterThan(0));
    fireEvent.change(getByLabelText("Key"), { target: { value: "call-started" } });
    await waitFor(() => expect(last(urls).searchParams.get("key")).toBe("call-started"));
    fireEvent.change(getByLabelText("Source"), { target: { value: "xapi" } });
    await waitFor(() => expect(last(urls).searchParams.get("source")).toBe("xapi"));
    fireEvent.change(getByLabelText("Message contains"), { target: { value: "dropped" } });
    await waitFor(() => expect(last(urls).searchParams.get("q")).toBe("dropped"));
    expect(last(urls).pathname).toMatch(/\/components\/p-1\/events$/);
  });

  it("logs: severity, source and text each narrow the read", async () => {
    const urls = captureRequests();
    const { getByLabelText, findByText } = mountList(new QueryClient({ defaultOptions: { queries: { retry: false } } }), () => <OccurrenceList kind="logs" scope={{ type: "system", name: "s-1" }} hint={LOG_HINT} />);
    await findByText(/no log lines in the last 24 hours/i);
    const severity = getByLabelText("Severity at least") as HTMLSelectElement;
    expect([...severity.options].map((o) => o.value)).toEqual(["", "emergency", "alert", "critical", "error", "warning", "notice", "info", "debug"]);
    fireEvent.change(severity, { target: { value: "warning" } });
    await waitFor(() => expect(last(urls).searchParams.get("severity")).toBe("warning"));
    await waitFor(() => expect([...(getByLabelText("Source") as HTMLSelectElement).options].map((o) => o.value)).toEqual(["", "nvp", "tcp"]));
    fireEvent.change(getByLabelText("Source"), { target: { value: "nvp" } });
    await waitFor(() => expect(last(urls).searchParams.get("source")).toBe("nvp"));
    fireEvent.change(getByLabelText("Message contains"), { target: { value: "reset" } });
    await waitFor(() => expect(last(urls).searchParams.get("q")).toBe("reset"));
    expect(last(urls).pathname).toMatch(/\/systems\/s-1\/logs$/);
  });

  it("says a filter matched nothing rather than that the component is silent", async () => {
    captureRequests();
    const { getByLabelText, findByText } = mountList(new QueryClient({ defaultOptions: { queries: { retry: false } } }), () => <OccurrenceList kind="logs" scope={{ type: "component", name: "p-1" }} hint={LOG_HINT} />);
    await findByText(/no log lines in the last 24 hours/i);
    fireEvent.change(getByLabelText("Message contains"), { target: { value: "zzz" } });
    expect(await findByText(/no line matches the filter/i)).toBeTruthy();
  });
});

describe("no page-specific occurrence markup remains", () => {
  const read = (f: string) => readFileSync(join(process.cwd(), "src", f), "utf8");
  it.each(["components/SystemDetail.tsx", "components/ComponentDetail.tsx", "pages/Outposts.tsx"])("%s lists events and logs only through OccurrenceList", (f) => {
    const src = read(f);
    expect(src).toContain("OccurrenceList");
    for (const banned of ["EventRow", "LogRow", "EventsPanel", "LogsPanel", "LogFilters", "systemEvents(", "systemLogs(", "getLogs(", "getEvents("]) {
      expect(src, `${f} still carries ${banned}`).not.toContain(banned);
    }
  });
});

describe("OccurrenceRow columns", () => {
  const log: Occurrence = { kind: "log", id: "l:0", ts: nowIso, badge: "info", badgeClass: "badge-ghost", owner: "disp-1", message: "retention swept 4 partitions", source: "scheduler", facility: "retention", attributes: { dropped: 4 } };
  const mount = (o: Occurrence, showSource: boolean) => render(() => <ul><OccurrenceRow o={o} showSource={showSource} /></ul>);

  it("a system row has a source column; a component row has none", () => {
    const sys = mount(log, true);
    expect(sys.queryByTestId("occurrence-source")?.textContent).toBe("disp-1");
    cleanup();
    const comp = mount(log, false);
    expect(comp.queryByTestId("occurrence-source")).toBeNull();
  });

  it("lays a log out as time, severity, facility, message and keeps the message to one line", async () => {
    const { getByTestId, getAllByTestId } = mount(log, false);
    expect(getByTestId("occurrence-time")).toBeTruthy();
    expect(getByTestId("occurrence-facility").textContent).toBe("retention");
    expect(getByTestId("occurrence-message").classList.contains("truncate")).toBe(true);
    expect(await tipOf(getByTestId("occurrence-message"))).toBe("retention swept 4 partitions");
    expect(getAllByTestId("occurrence-row")[0].querySelector("button")!.style.gridTemplateColumns).not.toBe("");
  });

  it("mutes info and keeps warning colored", () => {
    const a = mount(log, false);
    expect(a.getByText("info").className).toMatch(/text-base-content\/45/);
    expect(a.getByText("info").className).not.toMatch(/badge-(warning|error)/);
    cleanup();
    const b = mount({ ...log, badge: "warning", badgeClass: "badge-warning" }, false);
    expect(b.getByText("warning").classList.contains("badge-warning")).toBe(true);
  });

  it("expands in place on click, showing source, facility and attributes, with aria-expanded", () => {
    const { getByTestId, queryByTestId, getAllByRole } = mount(log, false);
    const btn = getAllByRole("button")[0];
    expect(btn.getAttribute("aria-expanded")).toBe("false");
    expect(queryByTestId("occurrence-detail")).toBeNull();
    fireEvent.click(btn);
    expect(btn.getAttribute("aria-expanded")).toBe("true");
    const d = getByTestId("occurrence-detail");
    expect(d.textContent).toContain("scheduler");
    expect(d.textContent).toContain("retention");
    expect(d.textContent).toContain('"dropped": 4');
  });
});
