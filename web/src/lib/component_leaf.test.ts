import { describe, expect, it } from "vitest";
import { collectionState, dotVerdict, identityRows, leafAlarmSince, membershipRows, vitalRows } from "./component_leaf";
import type { FleetView } from "./fleet";
import type { Node } from "./nodes";
import type { ReachEndpoint } from "./reachability";
import { uuidFor } from "./testids";
import { SEEDED_LEVELS } from "./testseverity";

// The component leaf's derivations (#637). The collection card's one job is
// the distinction the issue states plainly: a healthy node with a stale
// sample means the component or the path, not collection, which is why nodes do
// not need a zoom of their own.

const now = Date.parse("2026-08-15T12:00:00Z");
const iso = (secondsAgo: number) => new Date(now - secondsAgo * 1000).toISOString();

const iface = (verdict: { value: string; ts: string } | null, node?: string): ReachEndpoint =>
  ({ endpoint: "ssh", transport: "ssh", node, verdict, layers: [], history: [] }) as unknown as ReachEndpoint;

const node = (heartbeatSecondsAgo: number | null): Node =>
  ({ name: "edge-1", enrolled: true, last_heartbeat_at: heartbeatSecondsAgo === null ? undefined : iso(heartbeatSecondsAgo), tags: {} }) as Node;

describe("collectionState", () => {
  // A push endpoint (#1024) is never down for silence: no node collects it,
  // so neither a missing node nor an old last heard reads as offline or stale.
  it("a push endpoint listens, however long it has been silent", () => {
    const hook = { endpoint: "webhook", transport: "webhook", verdict: null, layers: [], history: [], last_seen: iso(86_400),
      webhook: { path: "/webhooks/x", scheme: "hmac", signed: true } } as unknown as ReachEndpoint;
    expect(collectionState(hook, undefined, now).kind).toBe("listening");
    expect(collectionState({ ...hook, last_seen: undefined }, undefined, now).kind).toBe("listening");
  });

  it("a fresh up verdict under a live node is collecting", () => {
    expect(collectionState(iface({ value: "up", ts: iso(10) }, "edge-1"), node(5), now).kind).toBe("collecting");
  });

  it("a stale sample under a LIVE node blames the component or the path, never collection", () => {
    const s = collectionState(iface({ value: "up", ts: iso(600) }, "edge-1"), node(5), now);
    expect(s.kind).toBe("component-or-path");
  });

  it("a stale sample under a dead node blames the node", () => {
    const s = collectionState(iface({ value: "up", ts: iso(600) }, "edge-1"), node(600), now);
    expect(s.kind).toBe("node-offline");
  });

  it("a fresh down verdict is the component answering badly, not a collection fault", () => {
    expect(collectionState(iface({ value: "down", ts: iso(10) }, "edge-1"), node(5), now).kind).toBe("down");
  });

  // An endpoint still answering reads collecting however long ago it last
  // changed state: the transition is old, the server's last-seen stamp is
  // not (#931).
  it("an old transition with a fresh last_seen is collecting", () => {
    const e = { ...iface({ value: "up", ts: iso(1260) }, "edge-1"), last_seen: iso(5) } as ReachEndpoint;
    expect(collectionState(e, node(5), now).kind).toBe("collecting");
  });

  // A driver endpoint never gets a reachability verdict; the batches its
  // task delivers (uptime, say) are what say it is collecting (#933).
  it("a driver endpoint with a fresh sample and no verdict is collecting", () => {
    const e = { ...iface(null, "edge-1"), last_seen: iso(5) } as ReachEndpoint;
    expect(collectionState(e, node(5), now).kind).toBe("collecting");
  });

  it("a driver endpoint whose samples stopped blames the component or the path", () => {
    const e = { ...iface(null, "edge-1"), last_seen: iso(600) } as ReachEndpoint;
    expect(collectionState(e, node(5), now).kind).toBe("component-or-path");
  });

  it("no verdict yet is unknown", () => {
    expect(collectionState(iface(null, "edge-1"), node(5), now).kind).toBe("unknown");
  });

  // A held session (#1059): the probes keep delivering while it is refused or
  // gone, so freshness alone would read collecting beside a session that
  // polls nothing. A rejected session stops polls; a down one fails them.
  const held = (state: "up" | "down" | "rejected") =>
    ({ ...iface({ value: "up", ts: iso(10) }, "edge-1"), last_seen: iso(3), session: { state, ts: iso(30) } }) as ReachEndpoint;
  it("a rejected session stops collection, however fresh the probes", () => {
    expect(collectionState(held("rejected"), node(5), now).kind).toBe("rejected");
  });
  it("a down session fails collection, however fresh the probes", () => {
    expect(collectionState(held("down"), node(5), now).kind).toBe("session-down");
  });
  it("an up session collects", () => {
    expect(collectionState(held("up"), node(5), now).kind).toBe("collecting");
  });

  // #1110: a gate the reachability probe set, on an endpoint holding no
  // session, stops the polls beneath it too (a stateless poll is sent at the
  // transport rung, an snmp one at the session rung, since every get carries
  // the community), so the card says polls stopped, not that the component
  // reported down. A login refused on a transport whose polls carry no
  // credential holds no poll, and the card keeps reading the verdict.
  const gated = (transport: string, rung: "responsive" | "authenticated") =>
    ({ ...iface({ value: "down", ts: iso(10) }, "edge-1"), transport, last_seen: iso(3), rejection: { rung, class: "auth", ts: iso(30) } }) as ReachEndpoint;
  // An http or https poll carries its driver's credential (#902), so a
  // refused credential holds the polls as an snmp community's refusal does,
  // and an https certificate gate holds them at the transport rung.
  it.each([
    ["snmp", "authenticated"],
    ["udp", "responsive"],
    ["tls", "responsive"],
    ["http", "authenticated"],
    ["https", "authenticated"],
    ["https", "responsive"],
  ] as const)("a %s probe's gate at the %s rung stops collection", (transport, rung) => {
    expect(collectionState(gated(transport, rung), node(5), now).kind).toBe("rejected");
  });
  it("an ssh probe's refused credential holds no poll: the verdict still reads", () => {
    expect(collectionState(gated("ssh", "authenticated"), node(5), now).kind).toBe("down");
  });
  // The node holds a stateless poll only where no session is held: a held
  // session's polls ride it, so a probe's gate beside an up session (a tls
  // certificate that expired under a long-lived connection) stops nothing,
  // and beside a down one the polls are tried and failing.
  it("a probe's gate beside a held session leaves its polls to the session", () => {
    const up = { ...gated("tls", "responsive"), verdict: { value: "up", ts: iso(10) }, session: { state: "up", ts: iso(30) } } as ReachEndpoint;
    expect(collectionState(up, node(5), now).kind).toBe("collecting");
    const down = { ...gated("tls", "responsive"), session: { state: "down", ts: iso(30) } } as ReachEndpoint;
    expect(collectionState(down, node(5), now).kind).toBe("session-down");
  });
  it("a node that stopped heartbeating still reads offline, whatever the session last said", () => {
    const e = { ...held("rejected"), last_seen: iso(600) } as ReachEndpoint;
    expect(collectionState(e, node(600), now).kind).toBe("node-offline");
  });
});

const view: FleetView = {
  locations: [
    { id: uuidFor("cl-l-huddle"), name: "huddle", label: "Huddle Room", location_type: "room", parent: null, verdict: "healthy" },
    { id: uuidFor("cl-l-201"), name: "class-201", label: "Class 201", location_type: "room", parent: null, verdict: "healthy" },
  ],
  systems: [
    // A system named for its room, the commonest naming in the field: the
    // room would only repeat the label, so the row carries no where.
    { id: uuidFor("cl-s-huddle"), name: "huddle", label: "Huddle Room", location: uuidFor("cl-l-huddle"), verdict: "healthy", dots: [] },
    { id: uuidFor("cl-s-201"), name: "meeting", label: "Meeting Room", location: uuidFor("cl-l-201"), verdict: "healthy", dots: [] },
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

  it("resolves two same-named systems by their uuids when the wire carries them", () => {
    const rows = membershipRows(
      [
        { component: "x", system: "twin", system_id: uuidFor("cl-s-a2"), primary: true, system_count: 2 },
        { component: "x", system: "twin", system_id: uuidFor("cl-s-a3"), primary: false, system_count: 2 },
      ] as never,
      view,
    );
    expect(rows.map((r) => r.label)).toEqual(["Twin 1", "Twin 2"]);
    // Neither twin is placed in this fixture, so no room to tell them apart by.
    expect(rows.map((r) => r.where)).toEqual([undefined, undefined]);
    expect(rows.map((r) => r.systemId)).toEqual([uuidFor("cl-s-a2"), uuidFor("cl-s-a3")]);
  });

  it("names the room beside a system, unless the room would only repeat the system's label", () => {
    const rows = membershipRows(
      [
        { component: "x", system: "meeting", system_id: uuidFor("cl-s-201"), primary: true, system_count: 2 },
        { component: "x", system: "huddle", system_id: uuidFor("cl-s-huddle"), primary: false, system_count: 2 },
      ] as never,
      view,
    );
    expect(rows[0]).toMatchObject({ label: "Meeting Room", where: "Class 201" });
    expect(rows[1]).toMatchObject({ label: "Huddle Room", where: undefined });
  });

  it("without a uuid on the wire, an ambiguous name stays undrillable rather than guessed", () => {
    const rows = membershipRows([{ component: "x", system: "twin", primary: true, system_count: 1 }] as never, view);
    expect(rows[0].systemId).toBeNull();
    expect(rows[0].label).toBe("twin");
  });
});

describe("the leaf's dispatch facts (#786)", () => {
  it("identityRows keeps only the properties that resolve to a value, label first", () => {
    const rows = identityRows([
      { property_type_name: "serial-number", label: "Serial Number", value: "SN-1183", is_set: true, from_contract: true },
      { property_type_name: "firmware-version", value: "2.1.4", is_set: false, from_contract: true },
      { property_type_name: "mac-address", value: null, is_set: false, from_contract: true },
    ] as never);
    expect(rows).toEqual([
      { name: "serial-number", label: "Serial Number", value: "SN-1183" },
      { name: "firmware-version", label: "firmware-version", value: "2.1.4" },
    ]);
  });

  it("identityRows names the calc rule a property's value was derived by (#999, #1018)", () => {
    const rows = identityRows([
      { property_type_name: "busy-flag", label: "Busy", value: true, is_set: false, from_contract: false, source_rule: "busy", source_rule_id: "11111111-1111-7111-8111-111111111111" },
    ] as never);
    expect(rows).toEqual([{ name: "busy-flag", label: "Busy", value: true, sourceRule: "busy", sourceRuleId: "11111111-1111-7111-8111-111111111111" }]);
  });

  it("vitalRows keeps only metrics carrying a value and marks the live series", () => {
    const rows = vitalRows([
      { metric_type_name: "icmp-rtt-avg", label: "RTT", data_type: "float", value: 6.1, is_sampled: true, from_contract: false, required: false },
      { metric_type_name: "target-volume", data_type: "float", value: 0.8, is_sampled: false, from_contract: true, required: false },
      { metric_type_name: "mic-gain", data_type: "float", is_sampled: false, from_contract: true, required: false },
      // A calculated value names the calc rule that derived it (#999).
      { metric_type_name: "temp-mean", data_type: "float", value: 21, is_sampled: true, from_contract: false, required: false, source_rule: "room-mean", source_rule_id: "11111111-1111-7111-8111-111111111111" },
    ] as never);
    expect(rows).toEqual([
      { name: "icmp-rtt-avg", label: "RTT", value: 6.1, sampled: true },
      { name: "target-volume", label: "target-volume", value: 0.8, sampled: false },
      { name: "temp-mean", label: "temp-mean", value: 21, sampled: true, sourceRule: "room-mean", sourceRuleId: "11111111-1111-7111-8111-111111111111" },
    ]);
  });

  it("leafAlarmSince answers since-when from the worst ACTIVE alarm, and null when nothing is wrong", () => {
    const now = Date.parse("2026-08-15T17:00:00Z");
    const alarms = [
      { id: "a1", severity: "warning", message: "Fan speed high", raised_at: "2026-08-15T16:00:00Z", active: true },
      { id: "a2", severity: "high", message: "No route to host", raised_at: "2026-08-15T14:20:00Z", active: true },
      { id: "a3", severity: "high", message: "Old outage", raised_at: "2026-08-01T00:00:00Z", active: false },
    ] as never;
    const s = leafAlarmSince(alarms, now, SEEDED_LEVELS)!;
    expect(s.message).toBe("No route to host");
    expect(s.ms).toBe(now - Date.parse("2026-08-15T14:20:00Z"));
    expect(leafAlarmSince([{ id: "a3", severity: "high", message: "x", raised_at: "2026-08-01T00:00:00Z", active: false }] as never, now)).toBeNull();
  });

  it("dotVerdict reads the component's verdict off any system's dots", () => {
    const v = {
      locations: [],
      systems: [
        { id: uuidFor("cl-dv-s1"), name: "a", label: "A", location: null, verdict: "degraded", dots: [{ component: uuidFor("cl-dv-c9"), name: "mic-1", verdict: "outage", primary: true, shared: false }] },
      ],
    } as never;
    expect(dotVerdict(v, uuidFor("cl-dv-c9"))).toBe("outage");
    expect(dotVerdict(v, "cX")).toBeNull();
  });
});
