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
      key: "call-started",
      event_type_id: "0192a5f0-1111-7000-8000-0000000000a1",
      origin: "caught",
      message: "call started with room-204",
      provenance: "observed",
      source: "xapi",
      attributes: { peer: "sip:room-204", protocol: "sip" },
    },
    {
      ts: nowIso,
      key: "meeting.ended",
      event_type_id: "0192a5f0-2222-7000-8000-0000000000a2",
      origin: "derived",
      message: "meeting ended (call dropped, no rejoin)",
      provenance: "observed",
      source: "rule",
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
    expect(getByText("call-started")).toBeTruthy();
    expect(getByText("call started with room-204")).toBeTruthy();
    expect(getByText("meeting.ended")).toBeTruthy();
    expect(getByText("meeting ended (call dropped, no rejoin)")).toBeTruthy();
  });

  it("discloses the attributes JSON only on demand", () => {
    const { getByText, queryByText } = mount();
    // The payload is hidden until the row's attributes disclosure is opened.
    expect(queryByText(/protocol/)).toBeNull();
    fireEvent.click(getByText("attributes"));
    expect(getByText(/"protocol": "sip"/)).toBeTruthy();
  });

  it("shows the empty state when a component has no events", () => {
    const { getByText } = mount({ component: "c2", events: [] });
    expect(getByText(/no events in the last 24 hours/i)).toBeTruthy();
  });
});
