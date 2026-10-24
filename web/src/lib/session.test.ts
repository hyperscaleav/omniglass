import { describe, it, expect, vi, beforeEach } from "vitest";
import { STALENESS_WINDOW_MS, type ReachEndpoint } from "./reachability";
import {
  ladder,
  rungHint,
  sessionWord,
  sessionShowsReason,
  retryRefusal,
  retryEndpoint,
  retryWaiting,
  RETRY_WAIT_MS,
  keyRefusal,
  keyNoun,
  trustOfferedKey,
  trustRefusal,
  KEY_CHANGE_CAUSES,
} from "./session";
import { uuidFor } from "./testids";

const now = Date.parse("2026-10-23T12:00:00Z");
const ago = (ms: number) => new Date(now - ms).toISOString();

const ep = (over: Partial<ReachEndpoint>): ReachEndpoint =>
  ({ id: uuidFor("ep-1"), endpoint: "telnet", transport: "telnet", verdict: null, layers: [], history: [], ...over }) as ReachEndpoint;
const ping = (value: number) => ({ layer: "ping", check: "icmp-reachable", value, ts: ago(5_000) });
const port = (value: number) => ({ layer: "port", check: "tcp-open", value, ts: ago(5_000) });

// A held session reads its state, aged (ADR-0150): a state is stored only on a
// transition, so an up session from last week is fine while the endpoint
// keeps delivering, and any state is stale once nothing has arrived for the
// staleness window.
describe("sessionWord", () => {
  it("is null for an endpoint that never held a session", () => {
    expect(sessionWord(ep({}), now)).toBeNull();
  });
  it("is the state while the endpoint keeps delivering, however old the transition", () => {
    const e = ep({ session: { state: "up", ts: ago(7 * 86_400_000) }, last_seen: ago(3_000) });
    expect(sessionWord(e, now)).toBe("up");
  });
  it("is stale once nothing has arrived for the staleness window", () => {
    const e = ep({ session: { state: "up", ts: ago(7 * 86_400_000) }, last_seen: ago(STALENESS_WINDOW_MS + 1_000) });
    expect(sessionWord(e, now)).toBe("stale");
  });
  it("ages by the transition itself before any last_seen stamp", () => {
    expect(sessionWord(ep({ session: { state: "rejected", ts: ago(10_000) } }), now)).toBe("rejected");
    expect(sessionWord(ep({ session: { state: "rejected", ts: ago(STALENESS_WINDOW_MS + 1) } }), now)).toBe("stale");
  });
});

// The reason is what an operator acts on, so it shows for the two states where
// it says something: down (why the node gave up for now) and rejected (what
// refused it). An up session's empty reason, or a recovering one's, is not
// news.
describe("sessionShowsReason", () => {
  it("shows for rejected and down with a reason", () => {
    expect(sessionShowsReason(ep({ session: { state: "rejected", ts: ago(1_000), reason: "auth: auth rejected" } }), now)).toBe(true);
    expect(sessionShowsReason(ep({ session: { state: "down", ts: ago(1_000), reason: "5 consecutive attempts failed" } }), now)).toBe(true);
  });
  it("hides for the other states and for an empty reason", () => {
    expect(sessionShowsReason(ep({ session: { state: "up", ts: ago(1_000), reason: "connected" } }), now)).toBe(false);
    expect(sessionShowsReason(ep({ session: { state: "recovering", ts: ago(1_000), reason: "connection lost: EOF" } }), now)).toBe(false);
    expect(sessionShowsReason(ep({ session: { state: "rejected", ts: ago(1_000) } }), now)).toBe(false);
  });
  it("still shows a rejected reason once stale: the last thing it said is what needs fixing", () => {
    expect(sessionShowsReason(ep({ session: { state: "rejected", ts: ago(STALENESS_WINDOW_MS + 1), reason: "auth: auth rejected" } }), now)).toBe(true);
  });
});

// The ladder (ADR-0158), bottom up: reachable is a ping and only informational,
// responsive is the transport and the gate, authenticated is the login.
describe("ladder", () => {
  it("reads a ping that failed beside a transport that answered: informational, not the gate", () => {
    const rungs = ladder(ep({ layers: [ping(0), port(1)], responsive: { value: "up", ts: ago(5_000) } }));
    expect(rungs.map((r) => [r.key, r.status, r.role])).toEqual([
      ["reachable", "fail", "informational"],
      ["responsive", "pass", "gate"],
    ]);
  });
  it("falls back to the tcp connect for the transport rung when no protocol answer was recorded", () => {
    const rungs = ladder(ep({ transport: "tcp", layers: [ping(1), port(0)] }));
    expect(rungs.map((r) => [r.key, r.status])).toEqual([
      ["reachable", "pass"],
      ["responsive", "fail"],
    ]);
  });
  it("reads an unmeasured rung as unknown rather than failed", () => {
    expect(ladder(ep({})).map((r) => [r.key, r.status])).toEqual([
      ["reachable", "unknown"],
      ["responsive", "unknown"],
    ]);
  });
  it("adds the login rung once a login was tried", () => {
    const rungs = ladder(ep({ layers: [ping(1)], responsive: { value: "up", ts: ago(1) }, authenticated: { value: "yes", ts: ago(1) } }));
    expect(rungs.map((r) => [r.key, r.status, r.role])).toEqual([
      ["reachable", "pass", "informational"],
      ["responsive", "pass", "gate"],
      ["authenticated", "pass", "login"],
    ]);
  });
  it("holds the rung an auth refusal gates, the rungs below still observed", () => {
    const rungs = ladder(
      ep({
        layers: [ping(1)],
        responsive: { value: "up", ts: ago(1) },
        authenticated: { value: "no", ts: ago(1) },
        rejection: { rung: "authenticated", class: "auth" },
      }),
    );
    expect(rungs.map((r) => [r.key, r.status])).toEqual([
      ["reachable", "pass"],
      ["responsive", "pass"],
      ["authenticated", "held"],
    ]);
  });
  it("holds the transport rung for a host-key refusal and leaves the login above it untried", () => {
    const rungs = ladder(
      ep({
        transport: "ssh",
        layers: [ping(1), port(1)],
        authenticated: { value: "yes", ts: ago(60_000) },
        rejection: { rung: "responsive", class: "host-key" },
      }),
    );
    expect(rungs.map((r) => [r.key, r.status])).toEqual([
      ["reachable", "pass"],
      ["responsive", "held"],
      ["authenticated", "untried"],
    ]);
  });
  // The login is attempted only once the transport answers (ADR-0158): when
  // the transport rung fails, the login above it was not tried this time, so
  // an earlier yes is not shown as current (an snmp agent gone silent, #821).
  it("reads the login above a failed transport rung as untried, not its last verdict", () => {
    const rungs = ladder(
      ep({
        transport: "snmp",
        layers: [ping(1)],
        responsive: { value: "down", ts: ago(1) },
        authenticated: { value: "yes", ts: ago(60_000) },
      }),
    );
    expect(rungs.map((r) => [r.key, r.status])).toEqual([
      ["reachable", "pass"],
      ["responsive", "fail"],
      ["authenticated", "untried"],
    ]);
  });
  it("shows the login rung a gate holds even before an auth verdict landed", () => {
    const rungs = ladder(ep({ responsive: { value: "up", ts: ago(1) }, rejection: { rung: "authenticated", class: "auth" } }));
    expect(rungs.at(-1)).toMatchObject({ key: "authenticated", status: "held" });
  });
  it("invents no login rung for a session that has no login, whatever its gate", () => {
    const rungs = ladder(ep({ transport: "tcp", layers: [ping(1), port(1)], session: { state: "rejected", ts: ago(1_000) }, rejection: { rung: "responsive", class: "protocol" } }));
    expect(rungs.map((r) => [r.key, r.status])).toEqual([
      ["reachable", "pass"],
      ["responsive", "held"],
    ]);
  });
  it("reads an icmp endpoint's ping as its gate, not as information", () => {
    expect(ladder(ep({ transport: "icmp", layers: [ping(1)] })).map((r) => [r.key, r.status, r.role])).toEqual([["reachable", "pass", "gate"]]);
  });
  it("teaches each rung in its hint, the informational ping included", () => {
    const [reach, resp] = ladder(ep({ layers: [ping(0), port(1)] }));
    expect(rungHint(reach)).toMatch(/informational/i);
    expect(rungHint(reach)).toMatch(/did not answer/i);
    expect(rungHint(resp)).toMatch(/gate/i);
    expect(rungHint(resp)).toMatch(/request answered/i);
  });
});

describe("retry", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("posts :retry through the generated client, by the endpoint's id", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ id: uuidFor("ep-1"), name: "telnet", transport: "telnet" }), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    await retryEndpoint(uuidFor("ep-1"));
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain(`/api/v1/endpoints/${uuidFor("ep-1")}:retry`);
  });

  // A retry is in hand until the session moves or the wait runs out, so a
  // node that never answers does not keep Retry spinning forever.
  it("holds a retry until the session moves or the wait runs out", () => {
    const pending = { ts: ago(30_000), at: now - 1_000 };
    expect(retryWaiting(pending, ago(30_000), now)).toBe(true);
    expect(retryWaiting(pending, ago(500), now)).toBe(false);
    expect(retryWaiting({ ...pending, at: now - RETRY_WAIT_MS - 1 }, ago(30_000), now)).toBe(false);
    expect(retryWaiting(null, ago(30_000), now)).toBe(false);
  });

  it("names the permission a caller lacks, and refuses nothing for one who holds it", () => {
    expect(retryRefusal({ permissions: ["*:read"] } as never)).toMatch(/endpoint:retry/);
    expect(retryRefusal({ permissions: ["endpoint:retry"] } as never)).toBeNull();
  });
});

// A refusal of a changed key (#1078): the gate carries both keys as data, so
// the console names them and trusts the exact offered one without reading the
// reason. Half a pair, or a refusal that is not about a key, is no evidence.
describe("keyRefusal", () => {
  const pinned = "SHA256:" + "A".repeat(43);
  const offered = "SHA256:" + "B".repeat(43);
  it("reads both keys and the class off the gate, never the reason", () => {
    const e = ep({
      session: { state: "rejected", ts: ago(1_000), reason: "host-key: the host key changed: pinned SHA256:xxx, offered SHA256:yyy" },
      rejection: { rung: "responsive", class: "host-key", pinned_key: pinned, offered_key: offered },
    });
    expect(keyRefusal(e)).toEqual({ class: "host-key", pinned, offered });
  });
  it("is null without both keys, or without a gate", () => {
    expect(keyRefusal(ep({ rejection: { rung: "responsive", class: "host-key", offered_key: offered } }))).toBeNull();
    expect(keyRefusal(ep({ rejection: { rung: "authenticated", class: "auth" } }))).toBeNull();
    expect(keyRefusal(ep({}))).toBeNull();
  });
  it("names the key by its class, so a certificate's pinned key reads as one", () => {
    expect(keyNoun("host-key")).toBe("host key");
    expect(keyNoun("certificate")).toBe("certificate key");
    expect(keyNoun("something-new")).toBe("key");
  });
  it("teaches the three ways a key changes, each with its own hint", () => {
    expect(KEY_CHANGE_CAUSES.map((c) => c.label)).toEqual(["Reinstalled", "Hardware swapped", "Man in the middle"]);
    for (const c of KEY_CHANGE_CAUSES) expect(c.hint.length).toBeGreaterThan(20);
  });
});

describe("trustOfferedKey", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("posts :retry with the exact key it is given as host_key", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ id: uuidFor("ep-1"), name: "ssh", transport: "ssh" }), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    const key = "SHA256:" + "B".repeat(43);
    await trustOfferedKey(uuidFor("ep-1"), key);
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain(`/api/v1/endpoints/${uuidFor("ep-1")}:retry`);
    expect(await req.json()).toEqual({ host_key: key });
  });

  it("throws the server's refusal, so the confirm can say why", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ title: "Conflict", status: 409, detail: "the component now offers SHA256:zzz" }), { status: 409, headers: { "Content-Type": "application/problem+json" } }),
    );
    await expect(trustOfferedKey(uuidFor("ep-1"), "SHA256:" + "B".repeat(43))).rejects.toMatchObject({ detail: expect.stringMatching(/now offers/) });
  });

  it("names the permission a caller lacks", () => {
    expect(trustRefusal({ permissions: ["*:read"] } as never)).toMatch(/endpoint:retry/);
    expect(trustRefusal({ permissions: ["endpoint:retry"] } as never)).toBeNull();
  });
});
