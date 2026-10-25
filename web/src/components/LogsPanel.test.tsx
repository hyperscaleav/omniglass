import { describe, it, expect } from "vitest";
import { render, fireEvent } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import LogsPanel from "./LogsPanel";
import { LOGS_KEY, NODE_LOGS_KEY, type ComponentLogs, type NodeLogs } from "../lib/logs";

// The panel is read-only: every row is a real API field, nothing derived. Data is
// seeded into the query cache so no server is needed.
const nowIso = new Date().toISOString();

const seed: ComponentLogs = {
  component: "disp-1",
  logs: [
    {
      ts: nowIso,
      source: "syslog",
      severity: "err",
      facility: "daemon",
      message: "codec unreachable",
      attributes: { code: 504 },
    },
    {
      ts: nowIso,
      source: "syslog",
      severity: "info",
      facility: "kern",
      message: "hdmi link state changed to up",
    },
  ],
};

function mount(data: ComponentLogs = seed) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...LOGS_KEY(data.component)], data);
  return render(() => (
    <QueryClientProvider client={qc}>
      <LogsPanel name={data.component} />
    </QueryClientProvider>
  ));
}

describe("LogsPanel", () => {
  it("renders one row per log line with its severity, message and source", () => {
    const { getByText, getAllByText } = mount();
    expect(getByText("2 in the last 24h")).toBeTruthy();
    expect(getByText("codec unreachable")).toBeTruthy();
    expect(getByText("err")).toBeTruthy();
    expect(getByText("hdmi link state changed to up")).toBeTruthy();
    expect(getAllByText("syslog").length).toBe(2);
  });

  // The disclosure names the schema it reveals (attributes, labels), matching the
  // events panel and the log_line columns. "fields" was overloaded (the secrets
  // list has a Fields column) and said nothing about which of the two you get.
  it("discloses the attributes only on demand, named for the column", () => {
    const { getByText, queryByText } = mount();
    expect(queryByText(/"code"/)).toBeNull();
    fireEvent.click(getByText("attributes"));
    expect(getByText(/"code": 504/)).toBeTruthy();
    expect(getByText("hide attributes")).toBeTruthy();
  });

  it("names the disclosure for whichever of attributes and labels the line carries", () => {
    const { getByText } = mount({
      component: "disp-1",
      logs: [
        { ts: nowIso, message: "classified only", labels: { class: "firmware" } },
        { ts: nowIso, message: "both", attributes: { code: 1 }, labels: { class: "kernel" } },
      ],
    });
    expect(getByText("labels")).toBeTruthy();
    expect(getByText("attributes + labels")).toBeTruthy();
  });

  it("captions each block once open, so the two are told apart", () => {
    const { getByText } = mount({
      component: "disp-1",
      logs: [{ ts: nowIso, message: "both", attributes: { code: 1 }, labels: { class: "kernel" } }],
    });
    fireEvent.click(getByText("attributes + labels"));
    expect(getByText(/"code": 1/)).toBeTruthy();
    expect(getByText(/"class": "kernel"/)).toBeTruthy();
  });

  it("shows the empty state when a component has no logs", () => {
    const { getByText } = mount({ component: "c2", logs: [] });
    expect(getByText(/no log lines in the last 24 hours/i)).toBeTruthy();
  });

  it("serves a node's self-logs from an explicit source and title", () => {
    const node: NodeLogs = {
      node: "site-a",
      logs: [{ ts: nowIso, source: "node", severity: "info", facility: "collection", message: "worklist pulled" }],
    };
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...NODE_LOGS_KEY(node.node)], node);
    const { getByText } = render(() => (
      <QueryClientProvider client={qc}>
        <LogsPanel name={node.node} title="Self-logs" queryKey={NODE_LOGS_KEY(node.node)} queryFn={() => Promise.resolve(node)} />
      </QueryClientProvider>
    ));
    expect(getByText("Self-logs")).toBeTruthy();
    expect(getByText("worklist pulled")).toBeTruthy();
    expect(getByText(/collection/)).toBeTruthy();
  });

  // A node's self-log read takes no filter: the controls belong to a
  // component's (and a system's) read alone.
  it("offers no filter on a node's self-logs", () => {
    const node: NodeLogs = { node: "site-b", logs: [{ ts: nowIso, message: "connected" }] };
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...NODE_LOGS_KEY(node.node)], node);
    const { queryByLabelText } = render(() => (
      <QueryClientProvider client={qc}>
        <LogsPanel name={node.node} queryKey={NODE_LOGS_KEY(node.node)} queryFn={() => Promise.resolve(node)} />
      </QueryClientProvider>
    ));
    expect(queryByLabelText("Severity at least")).toBeNull();
  });
});

// The filters (#1145): a severity threshold on RFC 5424's eight levels and the
// sources the window carries, each a narrowing the read applies server-side,
// so the filtered rows are the read's own answer under the filter's key.
describe("LogsPanel filters", () => {
  const unfiltered: ComponentLogs = {
    component: "panel-1",
    sources: ["nvp", "tcp"],
    logs: [
      { ts: nowIso, source: "tcp", severity: "warning", message: "backlight driver reset", instance: "tcp" },
      { ts: nowIso, source: "tcp", message: "EVT SCENE presentation", instance: "tcp" },
    ],
  };

  function mountFiltered() {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...LOGS_KEY("panel-1")], unfiltered);
    qc.setQueryData([...LOGS_KEY("panel-1", { severity: "warning" })], { ...unfiltered, logs: [unfiltered.logs[0]] });
    qc.setQueryData([...LOGS_KEY("panel-1", { source: "nvp" })], { ...unfiltered, logs: [] });
    return render(() => (
      <QueryClientProvider client={qc}>
        <LogsPanel name="panel-1" />
      </QueryClientProvider>
    ));
  }

  it("offers RFC 5424's levels and the window's sources", () => {
    const { getByLabelText } = mountFiltered();
    const severity = getByLabelText("Severity at least") as HTMLSelectElement;
    expect([...severity.options].map((o) => o.value)).toEqual(["", "emergency", "alert", "critical", "error", "warning", "notice", "info", "debug"]);
    const source = getByLabelText("Source") as HTMLSelectElement;
    expect([...source.options].map((o) => o.value)).toEqual(["", "nvp", "tcp"]);
  });

  it("reads the lines at a severity or worse", async () => {
    const { getByLabelText, findByText, queryByText } = mountFiltered();
    expect(queryByText("EVT SCENE presentation")).toBeTruthy();
    fireEvent.change(getByLabelText("Severity at least"), { target: { value: "warning" } });
    expect(await findByText("backlight driver reset")).toBeTruthy();
    expect(queryByText("EVT SCENE presentation")).toBeNull();
  });

  it("says a filter matched nothing rather than that the component is silent", async () => {
    const { getByLabelText, findByText } = mountFiltered();
    fireEvent.change(getByLabelText("Source"), { target: { value: "nvp" } });
    expect(await findByText(/no line matches the filter/i)).toBeTruthy();
  });

  it("names the endpoint that collected a line, and the source only where it differs", () => {
    const { getAllByTitle, queryByText } = mountFiltered();
    const via = getAllByTitle("The endpoint that collected the line");
    expect(via.map((e) => e.textContent)).toEqual(["via tcp", "via tcp"]);
    // Source and endpoint are both "tcp": the source is not said twice.
    expect(queryByText("tcp", { selector: "span.font-data:not([title] *)" })).toBeNull();
  });

  it("shows when a line arrived where its own time differs", () => {
    const own = new Date(Date.now() - 3 * 3600_000).toISOString();
    const { getByText } = mount({
      component: "disp-1",
      logs: [{ ts: own, received_at: nowIso, message: "booted before the node read it" }],
    });
    expect(getByText(/arrived/)).toBeTruthy();
  });
});
