import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import EventTypes from "./EventTypes";
import { EVENT_TYPES_KEY, type EventTypeRow } from "../lib/event_types";
import { ME_KEY, type Me } from "../lib/auth";

// The Event Types page is a single FlatList over the /event-types catalog. Official
// (seed-owned) event types are read-only; a custom event type is writable only when
// the caller holds event_type:create / event_type:update. Data is seeded into the
// query cache so no server is needed.
const seed: EventTypeRow[] = [
  { name: "syslog.line", display_name: "Syslog Line", official: true },
  { name: "call.started", display_name: "Call Started", official: true },
  { name: "cable.unplugged", display_name: "Cable unplugged", official: false },
];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...EVENT_TYPES_KEY], seed);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <EventTypes />
    </QueryClientProvider>
  ));
}

describe("Event Types page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists the seeded event types", () => {
    mount();
    expect(screen.getByText("syslog.line")).toBeTruthy();
    expect(screen.getByText("call.started")).toBeTruthy();
    expect(screen.getByText("cable.unplugged")).toBeTruthy();
  });

  it("shows New event type for a caller holding event_type:create", () => {
    mount(admin);
    expect(screen.getByText("New event type")).toBeTruthy();
  });

  it("hides New event type from a read-only viewer", () => {
    mount(viewer);
    expect(screen.queryByText("New event type")).toBeNull();
  });
});
