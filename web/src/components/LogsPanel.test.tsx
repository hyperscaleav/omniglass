import { describe, it, expect } from "vitest";
import { render, fireEvent } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import LogsPanel from "./LogsPanel";
import { LOGS_KEY, type ComponentLogs } from "../lib/logs";

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

  it("discloses the structured fields only on demand", () => {
    const { getByText, queryByText } = mount();
    expect(queryByText(/"code"/)).toBeNull();
    fireEvent.click(getByText("fields"));
    expect(getByText(/"code": 504/)).toBeTruthy();
  });

  it("shows the empty state when a component has no logs", () => {
    const { getByText } = mount({ component: "c2", logs: [] });
    expect(getByText(/no log lines in the last 24 hours/i)).toBeTruthy();
  });
});
