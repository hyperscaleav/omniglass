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
  { name: "call-started", display_name: "Call Started", official: true },
  { name: "cable-unplugged", display_name: "Cable unplugged", official: false },
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
    expect(screen.getByText("call-started")).toBeTruthy();
    expect(screen.getByText("cable-unplugged")).toBeTruthy();
  });

  // The identity cell puts the label on the primary line and the key beneath it, in
  // one cell, which is what makes this catalog read the same as every other list.
  it("renders the label above the key in a single identity cell", () => {
    mount();
    const cell = screen.getByText("call-started").closest("td");
    expect(cell?.textContent).toContain("Call Started");
    expect(screen.getAllByText("Call Started")).toHaveLength(1);
  });

  // One header word everywhere, "Name", even though this table's name is dotted
  // (call-started) rather than kebab. The separate Label column goes: the cell
  // already renders the display name, so the column only repeated it.
  it("keeps the Name header and drops the redundant Label column", () => {
    mount();
    expect(screen.getByRole("columnheader", { name: "Name" })).toBeTruthy();
    expect(screen.queryByRole("columnheader", { name: "Label" })).toBeNull();
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
