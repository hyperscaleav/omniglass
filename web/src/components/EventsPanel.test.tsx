import { describe, it, expect } from "vitest";
import { render, fireEvent, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
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
      <Router><Route path="*" component={() => <EventsPanel name={data.component} />} /></Router>
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

  it("names the rule that derived an event, linking to it, and opens the alarm it carries in place", () => {
    const { getByText } = mount({
      component: "disp-1",
      events: [
        {
          ts: nowIso,
          key: "condition-started",
          event_type_id: "0192a5f0-3333-7000-8000-0000000000a3",
          origin: "derived",
          message: "Reachability lost",
          provenance: "observed",
          source: "rule",
          instance: "tcp",
          derived_by_rule: "reachability-lost",
          derived_by_rule_id: "0192a5f0-5555-7000-8000-0000000000a5",
          alarm_id: "0192a5f0-4444-7000-8000-0000000000a4",
        },
      ],
    });
    const link = getByText("reachability-lost").closest("a")!;
    // By the rule's uuid: the name shown is the one the event was written
    // under, which a rename since may have moved (#977).
    expect(link.getAttribute("href")).toBe("/rules?rule=0192a5f0-5555-7000-8000-0000000000a5");
    const row = link.closest("div.flex-col") as HTMLElement;
    expect(within(row).getByRole("button", { name: /^Timeline of / })).toBeTruthy();
  });

  it("names a deleted rule as recorded, as plain text that says it is gone, never as a dead link", () => {
    const { getByText } = mount({
      component: "disp-1",
      events: [
        {
          ts: nowIso,
          key: "condition-ended",
          event_type_id: "0192a5f0-3333-7000-8000-0000000000a3",
          origin: "derived",
          message: "Old watch",
          provenance: "observed",
          source: "rule",
          derived_by_rule: "old-watch",
        },
      ],
    });
    const name = getByText("old-watch");
    expect(name.closest("a")).toBeNull();
    expect(name.closest("[title]")?.getAttribute("title")).toMatch(/deleted/);
  });
});
