import { describe, it, expect } from "vitest";
import {
  deviceReceived,
  figureText,
  gapName,
  gapTone,
  NOT_WIRED,
  ratioText,
  RESTARTED,
  simStateNote,
  sortDevices,
  sortGaps,
  WARMING_UP,
  wiredText,
  type PlatformSimDevice,
  type PlatformSimGap,
  nodePill,
  notUpCount,
  PLATFORM_REFUSED,
  platformColumns,
  platformRefusal,
  retryPlatformView,
  serverUptime,
  type PlatformView,
} from "./platformhealth";
import { NODE_DOWN_AFTER_MS } from "./nodes";

// The Platform health page's pure core (#1644): the columns, the uptime, a
// figure's text and the liveness pill, all derived from one view body and
// judged against that body's own clock, read_at, never the browser's.
const readAt = "2026-11-09T12:00:00Z";
const readMs = Date.parse(readAt);

const view = (over: Partial<PlatformView> = {}): PlatformView => ({
  read_at: readAt,
  lab: false,
  figures_read: true,
  servers: [],
  groups: [
    { name: "checkin", label: "Checkin", keys: [{ name: "checkin-age", label: "Check-in Age", unit: "s", description: "Seconds since the node last checked in." }] },
    {
      name: "commands",
      label: "Commands",
      keys: [
        { name: "command-pending-count", label: "Pending Commands", unit: "", description: "Commands waiting." },
        { name: "command-pending-age", label: "Oldest Pending Command", unit: "s", description: "Age of the oldest." },
      ],
    },
  ],
  nodes: [],
  ...over,
});

describe("platformColumns", () => {
  it("follows the groups the view returns, in order, with each key's catalog description", () => {
    const cols = platformColumns(view());
    expect(cols.map((c) => c.key)).toEqual(["checkin-age", "command-pending-count", "command-pending-age"]);
    expect(cols.map((c) => c.group)).toEqual(["Checkin", "Commands", "Commands"]);
    expect(cols[0].description).toBe("Seconds since the node last checked in.");
  });

  it("renders a group the page has never heard of, because the page holds no key list", () => {
    const v = view({ groups: [{ name: "pool", label: "Pool", keys: [{ name: "worker-busy-count", label: "Busy Workers" }] }] });
    expect(platformColumns(v).map((c) => c.key)).toEqual(["worker-busy-count"]);
  });

  it("is empty for a view with no groups", () => {
    expect(platformColumns(view({ groups: null }))).toEqual([]);
  });
});

describe("serverUptime", () => {
  it("is read_at minus started_at, not the browser clock", () => {
    const started = new Date(readMs - (3 * 3600 + 25 * 60) * 1000).toISOString();
    expect(serverUptime(readAt, started)).toBe("3h 25m");
  });

  it("is null before a boot has written its start time", () => {
    expect(serverUptime(readAt, undefined)).toBeNull();
  });
});

describe("figureText", () => {
  const node = { name: "edge-a", label: "", enrolled: true, figures: { "checkin-age": { value: 0, observed_at: readAt } } };

  it("renders a reported zero as zero, with its unit", () => {
    expect(figureText(node, { key: "checkin-age", unit: "s" })).toBe("0 s");
  });

  it("renders a figure the node never reported as not reported, never as 0", () => {
    expect(figureText(node, { key: "command-pending-count", unit: "" })).toBe("not reported");
  });

  it("renders a row without figures (no platform reach) as not reported", () => {
    expect(figureText({ ...node, figures: undefined }, { key: "checkin-age", unit: "s" })).toBe("not reported");
  });

  it("trims a fractional value to two places", () => {
    const n = { ...node, figures: { "checkin-age": { value: 2.3456, observed_at: readAt } } };
    expect(figureText(n, { key: "checkin-age", unit: "s" })).toBe("2.35 s");
  });
});

describe("nodePill", () => {
  const at = (ageMs: number) => ({ name: "edge-a", label: "", enrolled: true, last_heartbeat_at: new Date(readMs - ageMs).toISOString() });

  it("is nodeStatus at read_at: a heartbeat exactly the down window before reads up", () => {
    expect(nodePill(at(NODE_DOWN_AFTER_MS), readAt)).toBe("up");
  });

  it("reads down one millisecond past the window", () => {
    expect(nodePill(at(NODE_DOWN_AFTER_MS + 1), readAt)).toBe("down");
  });

  it("reads never for a node that has not checked in", () => {
    expect(nodePill({ name: "edge-new", label: "", enrolled: false }, readAt)).toBe("never");
  });

  it("counts the nodes whose pill is not up", () => {
    const v = view({ nodes: [at(1000), at(NODE_DOWN_AFTER_MS + 1), { name: "edge-new", label: "", enrolled: false }] });
    expect(notUpCount(v)).toBe(2);
  });
});


// A refusal is an answer, not an outage (#1644): a scoped node read is a 403
// the server will give again, so the query stops at once and says why, where
// a 5xx or a dropped connection is retried like any read.
describe("retryPlatformView", () => {
  it("never retries a 4xx refusal", () => {
    expect(retryPlatformView(0, { status: 403, title: "Forbidden" })).toBe(false);
    expect(retryPlatformView(0, { status: 401 })).toBe(false);
  });

  it("retries a server error or a network failure up to three times", () => {
    expect(retryPlatformView(0, { status: 500 })).toBe(true);
    expect(retryPlatformView(2, new TypeError("Failed to fetch"))).toBe(true);
    expect(retryPlatformView(3, { status: 503 })).toBe(false);
  });
});

describe("platformRefusal", () => {
  it("names the read a 403 lacks, not the server's bare forbidden", () => {
    expect(platformRefusal({ status: 403, title: "Forbidden", detail: "forbidden" })).toBe(PLATFORM_REFUSED);
    expect(PLATFORM_REFUSED).toMatch(/fleet-wide node read/);
  });

  it("is the error's own message otherwise", () => {
    expect(platformRefusal({ status: 500, title: "Internal Server Error", detail: "read platform view" })).toBe("read platform view");
  });
});

// With a simulator (#1647): order and tone only; every count is the server's.
const gap = (over: Partial<PlatformSimGap>): PlatformSimGap => ({
  gap: "sent-not-received", protocol: "http", count: 0, in: 100, ratio: 0, masked: 0, masked_by: "", answered_late: 0, reading: "loss", ...over,
});

describe("sortGaps", () => {
  it("puts the worst ratio first, sinks a gap with no ratio, and keeps travel order on a tie", () => {
    const got = sortGaps([
      gap({ gap: "connect-not-accepted", ratio: 0 }),
      gap({ gap: "sent-not-received", ratio: null }),
      gap({ gap: "received-not-settled", ratio: 0.02 }),
      gap({ gap: "answered-not-returned", ratio: 0 }),
      gap({ gap: "published-not-landed", ratio: 0.5 }),
    ]).map((g) => g.gap);
    expect(got).toEqual(["published-not-landed", "received-not-settled", "connect-not-accepted", "answered-not-returned", "sent-not-received"]);
  });
  it("reads a warming-up node's null gaps as none", () => {
    expect(sortGaps(null)).toEqual([]);
  });
});

describe("gapTone", () => {
  it("maps impaired, restarted and unexplained loss to distinct tones", () => {
    const tones = [gapTone({ reading: "impaired", ratio: 0.3 }), gapTone({ reading: "restarted", ratio: null }), gapTone({ reading: "loss", ratio: 0.3 })];
    expect(tones).toEqual(["expected", "restarted", "fault"]);
    expect(new Set(tones).size).toBe(3);
  });
  it("tints a ratio only above 0", () => {
    expect(gapTone({ reading: "loss", ratio: 0 })).toBe("calm");
    expect(gapTone({ reading: "impaired", ratio: 0 })).toBe("calm");
    expect(gapTone({ reading: "loss", ratio: null })).toBe("calm");
    expect(gapTone({ reading: "not observed by the sim", ratio: null })).toBe("calm");
  });
});

describe("gap words", () => {
  it("names the hop and prints the ratio as a percent, no number when there is none", () => {
    expect(gapName("sent-not-received")).toBe("sent, not received");
    expect(gapName("spool-dropped")).toBe("spool dropped");
    expect(ratioText(0)).toBe("0%");
    expect(ratioText(0.125)).toBe("12.5%");
    expect(ratioText(0.0004)).toBe("0.04%");
    expect(ratioText(null)).toBe("-");
  });
});

describe("states with no number", () => {
  it("says why a warming-up or restarted window has no figures, naming no number", () => {
    expect(simStateNote({ state: "warming up" })).toBe(WARMING_UP);
    expect(simStateNote({ state: "restarted" })).toBe(RESTARTED);
    expect(simStateNote({ state: "measured" })).toBeNull();
    expect(WARMING_UP).not.toMatch(/\d/);
    expect(RESTARTED).not.toMatch(/\d/);
  });
  it("reads an unreported queue depth or busy-worker figure as not wired yet", () => {
    expect(wiredText(null)).toBe(NOT_WIRED);
    expect(wiredText(undefined)).toBe(NOT_WIRED);
    expect(wiredText(0)).toBe("0");
    expect(NOT_WIRED).not.toMatch(/\d/);
  });
});

describe("devices", () => {
  const dev = (over: Partial<PlatformSimDevice>): PlatformSimDevice => ({ component_id: "c", name: "", received: {}, impaired: false, unhit: false, ...over });
  it("sums a device's requests over its protocols", () => {
    expect(deviceReceived(dev({ received: { http: 4, tcp: 2 } }))).toBe(6);
    expect(deviceReceived(dev({}))).toBe(0);
  });
  it("lists unhit, then impaired, then the rest by name", () => {
    const got = sortDevices([dev({ name: "b" }), dev({ name: "a" }), dev({ name: "z", impaired: true }), dev({ name: "y", unhit: true })]).map((d) => d.name);
    expect(got).toEqual(["y", "z", "a", "b"]);
  });
});
