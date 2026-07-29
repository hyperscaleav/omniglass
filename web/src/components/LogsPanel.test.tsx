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
});
