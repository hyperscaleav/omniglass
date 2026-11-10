import { describe, it, expect } from "vitest";
import {
  figureText,
  nodePill,
  PANEL_SERIES_HOURS,
  PANEL_SERIES_RESOLUTION_S,
  panelAlarms,
  panelFlagged,
  panelRowText,
  panelSections,
  panelSeriesRequest,
  type PlatformServer,
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

// A server's panels (#1645): Requests, Ingest, Queues and Retention, each the
// keys the view returns in the order it returns them. The page ranks nothing
// and filters nothing: a ranked key's rows are the instances the server cut
// and ordered, and a key read against another prints as a pair.
const server = (over: Partial<PlatformServer> = {}): PlatformServer => ({
  component: "c0ffee00-0000-4000-8000-000000000001",
  name: "lab-server",
  verdict: "healthy",
  alarms: [],
  groups: [
    {
      name: "requests",
      label: "Requests",
      keys: [
        { name: "http-request-duration-max", label: "Slowest Request", unit: "ms", description: "The slowest request in the window.", rank: 10 },
        { name: "db-pool-acquired", label: "Pool In Use", unit: "", description: "Connections in use.", of: "db-pool-max" },
        { name: "db-pool-max", label: "Pool Size", unit: "", description: "The pool's size." },
        { name: "db-slow-query-count", label: "Slow Queries", unit: "", description: "Slow queries in the window." },
      ],
      figures: [
        { key: "http-request-duration-max", instance: "GET /api/v1/components/{name} 2xx", value: 812.5, observed_at: readAt },
        { key: "http-request-duration-max", instance: "GET /api/v1/fast 2xx", value: 3, observed_at: readAt },
        { key: "db-pool-acquired", instance: "", value: 4, observed_at: readAt },
        { key: "db-pool-max", instance: "", value: 20, observed_at: readAt },
      ],
    },
    {
      name: "retention",
      label: "Retention",
      keys: [
        { name: "retention-behind", label: "Retention Behind", unit: "d", description: "Days past due." },
        { name: "metric-oldest-day-age", label: "Oldest Metric Day", unit: "d", description: "Age of the oldest day.", of: "retention-window-days" },
        { name: "retention-window-days", label: "Retention Window", unit: "d", description: "The window." },
      ],
      figures: [
        { key: "retention-behind", instance: "event", value: 0, observed_at: readAt },
        { key: "retention-behind", instance: "metric", value: 1, observed_at: readAt },
        { key: "metric-oldest-day-age", instance: "", value: 6, observed_at: readAt },
        { key: "retention-window-days", instance: "metric", value: 7, observed_at: readAt },
      ],
    },
  ],
  ...over,
});

describe("panelSections", () => {
  it("renders the keys and the ranked instances the view returns, in order, folding a pair's second key into the first", () => {
    const [requests] = server().groups!;
    const sections = panelSections(requests);
    expect(sections.map((s) => s.key.name)).toEqual(["http-request-duration-max", "db-pool-acquired", "db-slow-query-count"]);
    const ranked = sections[0];
    expect(ranked.ranked).toBe(true);
    expect(ranked.rows.map((r) => r.instance)).toEqual(["GET /api/v1/components/{name} 2xx", "GET /api/v1/fast 2xx"]);
    expect(ranked.rows.map((r) => r.value)).toEqual([812.5, 3]);
  });

  it("gives an unsplit key one row and a key never sampled one not-reported row", () => {
    const sections = panelSections(server().groups![0]);
    expect(sections[1].rows).toHaveLength(1);
    expect(sections[1].rows[0]).toMatchObject({ instance: "", value: 4, of: { value: 20 } });
    expect(sections[2].rows).toEqual([expect.objectContaining({ key: "db-slow-query-count", instance: "", value: null })]);
  });

  it("splits a key by instance and reads a pair's second figure from the sole series it has", () => {
    const sections = panelSections(server().groups![1]);
    expect(sections[0].rows.map((r) => r.instance)).toEqual(["event", "metric"]);
    expect(sections[1].rows[0].of).toMatchObject({ value: 7, unit: "d" });
  });

  it("is a not-reported row per key for a server with no samples yet", () => {
    const bare = { ...server().groups![0], figures: [] };
    const sections = panelSections(bare);
    expect(sections.flatMap((s) => s.rows.map((r) => r.value))).toEqual([null, null, null]);
  });
});

describe("panelRowText", () => {
  it("prints a figure with its unit, a pair as a / b, and a missing figure as not reported", () => {
    const sections = panelSections(server().groups![0]);
    expect(panelRowText(sections[0].rows[0])).toBe("812.5 ms");
    expect(panelRowText(sections[1].rows[0])).toBe("4 / 20");
    expect(panelRowText(sections[2].rows[0])).toBe("not reported");
  });

  it("prints a byte figure in the largest unit that keeps it short, each side of a pair alike", () => {
    expect(panelRowText({ key: "database-size", instance: "", unit: "bytes", value: 18_192_863 })).toBe("17.4 MB");
    expect(panelRowText({ key: "table-bytes", instance: "metric", unit: "bytes", value: 512 })).toBe("512 B");
    expect(
      panelRowText({
        key: "used-bytes",
        instance: "",
        unit: "bytes",
        value: 1_048_576,
        of: { key: "total-bytes", unit: "bytes", value: 4_194_304 },
      }),
    ).toBe("1.0 MB / 4.0 MB");
  });
});

describe("panelSeriesRequest", () => {
  it("asks for a ranked row's own instance on the server's platform component, over the last hour", () => {
    const s = server();
    const row = panelSections(s.groups![0])[0].rows[1];
    expect(panelSeriesRequest(s, row)).toEqual({
      kind: "components",
      owner: s.component,
      metric: "http-request-duration-max",
      hours: PANEL_SERIES_HOURS,
      resolutionSeconds: PANEL_SERIES_RESOLUTION_S,
      instance: "GET /api/v1/fast 2xx",
    });
    expect(PANEL_SERIES_HOURS).toBe(1);
  });

  it("names the owner-level series (the empty instance) for an unsplit key, never leaving it to the server to pick", () => {
    const s = server();
    expect(panelSeriesRequest(s, panelSections(s.groups![0])[1].rows[0]).instance).toBe("");
  });
});

describe("panelFlagged", () => {
  const alarm = (key: string | undefined) => ({ rule: "ingest-lag-high", key, severity: "warning", message: "lag high", raised_at: readAt });

  it("flags a panel only when an open alarm names one of its keys", () => {
    const s = server({ alarms: [alarm("retention-behind")] });
    const [requests, retention] = s.groups!;
    expect(panelFlagged(s, requests)).toBe(false);
    expect(panelFlagged(s, retention)).toBe(true);
    expect(panelAlarms(s, retention).map((a) => a.message)).toEqual(["lag high"]);
  });

  it("does not flag for an alarm on another key, a hand-raised alarm, or no alarms at all", () => {
    expect(panelFlagged(server({ alarms: [alarm("ingest-lag-max")] }), server().groups![0])).toBe(false);
    expect(panelFlagged(server({ alarms: [alarm(undefined)] }), server().groups![0])).toBe(false);
    expect(panelFlagged(server({ alarms: null }), server().groups![0])).toBe(false);
  });

  it("flags on the second key of a pair, which the panel carries though it prints no row of its own", () => {
    const s = server({ alarms: [alarm("retention-window-days")] });
    expect(panelFlagged(s, s.groups![1])).toBe(true);
  });
});
