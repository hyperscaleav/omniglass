import { describe, it, expect } from "vitest";
import { render, fireEvent } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import EventsPanel from "./EventsPanel";
import { EVENTS_KEY, type ComponentEvents } from "../lib/events";

// The panel is read-only: every row is a real API field, nothing derived. Data is
// seeded into the query cache so no server is needed.
const nowIso = new Date().toISOString();

const seed: ComponentEvents = {
  component: "disp-1",
  events: [
    {
      ts: nowIso,
      key: "syslog.line",
      property_id: "0192a5f0-1111-7000-8000-0000000000a1",
      instance: "eth0",
      message: "link state changed to up",
      provenance: "observed",
      source: "syslog",
      attributes: { severity: "info", facility: "kern" },
    },
    {
      ts: nowIso,
      key: "snmp.trap",
      property_id: "0192a5f0-2222-7000-8000-0000000000a2",
      message: "coldStart",
      provenance: "observed",
      source: "snmp",
    },
  ],
};

function mount(data: ComponentEvents = seed) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...EVENTS_KEY(data.component)], data);
  return render(() => (
    <QueryClientProvider client={qc}>
      <EventsPanel name={data.component} />
    </QueryClientProvider>
  ));
}

describe("EventsPanel", () => {
  it("renders one row per event with its key, message and source", () => {
    const { getByText } = mount();
    expect(getByText("2 in the last 24h")).toBeTruthy();
    expect(getByText("syslog.line")).toBeTruthy();
    expect(getByText("link state changed to up")).toBeTruthy();
    expect(getByText("snmp.trap")).toBeTruthy();
    expect(getByText("coldStart")).toBeTruthy();
  });

  it("discloses the attributes JSON only on demand", () => {
    const { getByText, queryByText } = mount();
    // The payload is hidden until the row's attributes disclosure is opened.
    expect(queryByText(/facility/)).toBeNull();
    fireEvent.click(getByText("attributes"));
    expect(getByText(/"facility": "kern"/)).toBeTruthy();
  });

  it("shows the empty state when a component has no events", () => {
    const { getByText } = mount({ component: "c2", events: [] });
    expect(getByText(/no events in the last 24 hours/i)).toBeTruthy();
  });
});
