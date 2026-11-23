import { describe, it, expect, vi, beforeEach } from "vitest";
import { classifyPollFailure, pollNames, readSample, runEndpointPoll, testPollRefusal } from "./poll_test";

// An operator's test of an endpoint's poll (#2427): the data layer holds what
// the run answered and what each refusal means, so the component only draws.

beforeEach(() => vi.restoreAllMocks());

const problem = (status: number, detail: string) => ({ status, detail, title: "x" });

describe("runEndpointPoll", () => {
  it("posts to the poll's :run route and returns what it read", async () => {
    const body = { endpoint: "http", outpost: "edge-hq", stored: true, poll: "identity", samples: [{ name: "model-number", text: "PDU 4C", ts: "2026-11-23T14:00:00Z" }], took_ms: 2 };
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } }));
    const run = await runEndpointPoll("ep-1", "identity");
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain("/api/v1/endpoints/ep-1/polls/identity:run");
    expect(run.took_ms).toBe(2);
    expect(run.samples).toHaveLength(1);
  });

  it("throws the problem the server answered, status and detail intact", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify(problem(409, "the endpoint is gated")), { status: 409, headers: { "Content-Type": "application/problem+json" } }),
    );
    await expect(runEndpointPoll("ep-1", "identity")).rejects.toMatchObject({ status: 409, detail: "the endpoint is gated" });
  });
});

describe("classifyPollFailure", () => {
  it("reads a gate as held, keeping the gate's own words", () => {
    const f = classifyPollFailure(problem(409, "the endpoint is gated, so the poll was not sent: auth: the agent answered authorizationError for the community; fix what refused it, or retry the endpoint (retry-endpoint)"));
    expect(f.kind).toBe("gated");
    expect(f.reason).toMatch(/^auth: the agent answered authorizationError/);
  });
  it("reads a held probe reason as gated", () => {
    const f = classifyPollFailure(problem(409, "the endpoint is held, so the poll was not sent: unreachable"));
    expect(f).toMatchObject({ kind: "gated", reason: "unreachable" });
  });
  it("reads an endpoint on no outpost, and an outpost not connected, as no outpost", () => {
    expect(classifyPollFailure(problem(409, 'endpoint "http" is placed on no outpost, so no outpost can run its poll')).kind).toBe("no-outpost");
    expect(classifyPollFailure(problem(503, "the outpost is not connected, so it cannot run this now")).kind).toBe("no-outpost");
  });
  it("reads the budget, the limiter and a bare failure", () => {
    expect(classifyPollFailure(problem(504, "the outpost did not answer within the run's budget")).kind).toBe("timeout");
    expect(classifyPollFailure(problem(429, "too many on-demand runs")).kind).toBe("busy");
    expect(classifyPollFailure(problem(502, "the outpost could not run the poll: dial tcp: refused")).kind).toBe("failed");
    expect(classifyPollFailure(new Error("network down")).kind).toBe("failed");
  });
  it("a plain conflict is not a gate", () => {
    expect(classifyPollFailure(problem(409, "the poll is not on its outpost's worklist (disabled, or moved)")).kind).toBe("failed");
  });
});

describe("pollNames", () => {
  it("lists the driver's polls, and none for no driver or a spec with none", () => {
    expect(pollNames({ spec: { version: 1, transport: "http", polls: [{ name: "identity" }, { name: "status" }] } } as never)).toEqual(["identity", "status"]);
    expect(pollNames(undefined)).toEqual([]);
    expect(pollNames({ spec: { version: 1, transport: "http" } } as never)).toEqual([]);
  });
});

describe("readSample", () => {
  const metric = { name: "uptime", unit: "s", precision: 0 } as never;
  it("a metric reads its number with the metric type's unit and precision", () => {
    expect(readSample({ name: "uptime", value: 538.4, ts: "t" }, metric)).toMatchObject({ name: "uptime", text: "538", unit: "s" });
    expect(readSample({ name: "temp", value: 21.456, ts: "t" }, { name: "temp", precision: 1 } as never).text).toBe("21.5");
  });
  it("a metric of no known type reads as it came", () => {
    expect(readSample({ name: "x", value: 3, ts: "t" }, undefined)).toMatchObject({ text: "3" });
  });
  it("a property reads its text, an event its message with its lane", () => {
    expect(readSample({ name: "model-number", text: "PDU 4C", ts: "t" }, undefined).text).toBe("PDU 4C");
    expect(readSample({ name: "boot", text: "started", lane: "event", ts: "t" }, undefined).lane).toBe("event");
  });
});

describe("testPollRefusal", () => {
  it("names endpoint:run for a caller without it, nothing for one with it", () => {
    expect(testPollRefusal({ permissions: ["*:read"] } as never)).toMatch(/endpoint:run/);
    expect(testPollRefusal({ permissions: ["endpoint:run"] } as never)).toBeNull();
  });
});
