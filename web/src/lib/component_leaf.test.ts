import { describe, expect, it } from "vitest";
import { collectionState, membershipRows } from "./component_leaf";
import type { FleetView } from "./fleet";
import type { Node } from "./nodes";
import type { ReachInterface } from "./reachability";
import { uuidFor } from "./testids";

// The component leaf's derivations (#637). The collection card's one job is
// the distinction the issue states plainly: a healthy node with a stale
// sample means the device or the path, not collection, which is why nodes do
// not need a zoom of their own.

const now = Date.parse("2026-08-15T12:00:00Z");
const iso = (secondsAgo: number) => new Date(now - secondsAgo * 1000).toISOString();

const iface = (verdict: { value: string; ts: string } | null, node?: string): ReachInterface =>
  ({ interface: "ssh", interface_type: "ssh", node, verdict, layers: [], history: [] }) as unknown as ReachInterface;

const node = (heartbeatSecondsAgo: number | null): Node =>
  ({ name: "edge-1", enrolled: true, last_heartbeat_at: heartbeatSecondsAgo === null ? undefined : iso(heartbeatSecondsAgo), tags: {} }) as Node;

describe("collectionState", () => {
  it("a fresh up verdict under a live node is collecting", () => {
    expect(collectionState(iface({ value: "up", ts: iso(10) }, "edge-1"), node(5), now).kind).toBe("collecting");
  });

  it("a stale sample under a LIVE node blames the device or the path, never collection", () => {
    const s = collectionState(iface({ value: "up", ts: iso(600) }, "edge-1"), node(5), now);
    expect(s.kind).toBe("device-or-path");
  });

  it("a stale sample under a dead node blames the node", () => {
    const s = collectionState(iface({ value: "up", ts: iso(600) }, "edge-1"), node(600), now);
    expect(s.kind).toBe("node-offline");
  });

  it("a fresh down verdict is the device answering badly, not a collection fault", () => {
    expect(collectionState(iface({ value: "down", ts: iso(10) }, "edge-1"), node(5), now).kind).toBe("down");
  });

  it("no verdict yet is unknown", () => {
    expect(collectionState(iface(null, "edge-1"), node(5), now).kind).toBe("unknown");
  });
});

const view: FleetView = {
  locations: [],
  systems: [
    { id: uuidFor("cl-s-a"), name: "boardroom", label: "Boardroom A System", location: null, verdict: "healthy", dots: [] },
    { id: uuidFor("cl-s-b"), name: "overflow", label: "Overflow", location: null, verdict: "healthy", dots: [] },
    // A second system sharing the name of the first: the bare name is
    // ambiguous, so no uuid resolves for it.
    { id: uuidFor("cl-s-a2"), name: "twin", label: "Twin 1", location: null, verdict: "healthy", dots: [] },
    { id: uuidFor("cl-s-a3"), name: "twin", label: "Twin 2", location: null, verdict: "healthy", dots: [] },
  ],
} as unknown as FleetView;

describe("membershipRows", () => {
  it("marks the primary and resolves a unique system name to its uuid for the drill", () => {
    const rows = membershipRows(
      [
        { component: "bar-1", system: "boardroom", primary: true, system_count: 2 },
        { component: "bar-1", system: "overflow", primary: false, system_count: 2 },
      ] as never,
      view,
    );
    expect(rows[0]).toMatchObject({ system: "boardroom", primary: true, systemId: uuidFor("cl-s-a"), label: "Boardroom A System" });
    expect(rows[1].primary).toBe(false);
  });

  it("leaves an ambiguous system name undrillable rather than guessing", () => {
    const rows = membershipRows([{ component: "x", system: "twin", primary: true, system_count: 1 }] as never, view);
    expect(rows[0].systemId).toBeNull();
    expect(rows[0].label).toBe("twin");
  });
});
