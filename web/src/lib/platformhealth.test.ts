import { describe, it, expect } from "vitest";
import {
  deviceReceived,
  figureText,
  gapName,
  gapProtocolText,
  gapTone,
  NOT_WIRED,
  ratioText,
  RESTARTED,
  windowText,
  simStateNote,
  sortDevices,
  sortGaps,
  WARMING_UP,
  wiredText,
  type PlatformSimDevice,
  type PlatformSimGap,
  hopFigure,
  lossPercent,
  lossTinted,
  nodeLossHint,
  nodeLossText,
  nodePill,
  pipelineKinds,
  endToEndFigure,
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
  type PlatformPipeline,
  type PlatformPipelineHop,
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
  loss_hops: ["node"],
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

  it("names what a gap with no protocol counts: every listener for a connect, batches for the telemetry hops", () => {
    expect(gapProtocolText({ gap: "sent-not-received", protocol: "snmp" })).toBe("snmp");
    expect(gapProtocolText({ gap: "connect-not-accepted", protocol: "" })).toBe("any listener");
    expect(gapProtocolText({ gap: "published-not-landed", protocol: "" })).toBe("batches");
    expect(gapProtocolText({ gap: "spool-dropped", protocol: "" })).toBe("batches");
  });
});

describe("windowText", () => {
  it("names the window in minutes, or in seconds for a lab's short one", () => {
    expect(windowText(600)).toBe("10 minutes");
    expect(windowText(60)).toBe("1 minute");
    expect(windowText(100)).toBe("100 seconds");
    expect(windowText(20)).toBe("20 seconds");
    expect(windowText(undefined)).toBe("10 minutes");
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
  const dev = (over: Partial<PlatformSimDevice>): PlatformSimDevice => ({ component_id: "c", component: "", received: {}, impaired: false, unhit: false, ...over });
  it("sums a device's requests over its protocols", () => {
    expect(deviceReceived(dev({ received: { http: 4, tcp: 2 } }))).toBe(6);
    expect(deviceReceived(dev({}))).toBe(0);
  });
  it("lists unhit, then impaired, then the rest by name", () => {
    const got = sortDevices([dev({ component: "b" }), dev({ component: "a" }), dev({ component: "z", impaired: true }), dev({ component: "y", unhit: true })]).map((d) => d.component);
    expect(got).toEqual(["y", "z", "a", "b"]);
  });
});

// Where work is lost (#1646): every figure is a loss ratio, 0 is healthy, and
// nothing in is no ratio at all, never a fake 0.
describe("lossPercent", () => {
  it("reads a zero ratio as 0%", () => {
    expect(lossPercent(0)).toBe("0%");
  });

  it("keeps two significant figures, so a small loss is never rounded to 0", () => {
    expect(lossPercent(0.01)).toBe("1%");
    expect(lossPercent(0.0002)).toBe("0.02%");
    expect(lossPercent(1 / 3)).toBe("33%");
    expect(lossPercent(1)).toBe("100%");
  });
});

describe("lossTinted", () => {
  it("tints a ratio above 0 only", () => {
    expect(lossTinted(0)).toBe(false);
    expect(lossTinted(undefined)).toBe(false);
    expect(lossTinted(0.0001)).toBe(true);
  });
});

const fig = (inn: number, out: number) => ({ in: inn, out, ...(inn > 0 ? { loss: (inn - out) / inn } : {}) });
const span = (inn: number, out: number) => ({ newest: fig(inn, out), last_hour: fig(inn, out) });
const hop = (over: Partial<PlatformPipelineHop> = {}): PlatformPipelineHop => ({
  id: "node",
  label: "Node",
  description: "The node's spool.",
  component_class: "node",
  keys: ["delivery-in-count", "delivery-out-count", "delivery-loss-ratio"],
  kinds: [
    { kind: "metric", ...span(100, 99) },
    { kind: "log", ...span(0, 0) },
  ],
  total: span(100, 99),
  by_component: [],
  ...over,
});

describe("hopFigure", () => {
  it("reads every kind as the hop's total and one kind as its row, over the last hour", () => {
    expect(hopFigure(hop(), "all")).toEqual(fig(100, 99));
    expect(hopFigure(hop(), "metric")).toEqual(fig(100, 99));
    expect(hopFigure(hop(), "log")).toEqual({ in: 0, out: 0 });
  });

  it("is absent for a kind the hop carried nothing of, and for a hop with no rows", () => {
    expect(hopFigure(hop(), "event")).toBeUndefined();
    expect(hopFigure(hop({ kinds: [], total: span(0, 0) }), "all")).toBeUndefined();
  });
});

describe("pipelineKinds and endToEndFigure", () => {
  const p: PlatformPipeline = {
    keys: [],
    hops: [hop(), hop({ id: "ingest", label: "Ingest", component_class: "server", kinds: [{ kind: "event", ...span(4, 2) }, { kind: "metric", ...span(99, 99) }] })],
    end_to_end: [{ kind: "metric", newest: 0.01, last_hour: 0.01 }, { kind: "event", newest: 0.5, last_hour: 0.5 }],
    end_to_end_total: { newest: 0.02, last_hour: 0.03 },
  };

  it("lists the kinds any hop carried, in the delivery model's order", () => {
    expect(pipelineKinds(p)).toEqual(["metric", "event", "log"]);
  });

  it("reads the end to end for every kind or one kind, over the last hour", () => {
    expect(endToEndFigure(p, "all")).toBe(0.03);
    expect(endToEndFigure(p, "event")).toBe(0.5);
    expect(endToEndFigure(p, "property")).toBeUndefined();
  });
});

// A node's Loss group is its composed ratio over its own hops; a node that
// recorded nothing reads not reported, never 0.
describe("nodeLossText", () => {
  const node = { name: "edge-a", label: "", enrolled: true };

  it("shows the node's composed ratio", () => {
    expect(nodeLossText({ ...node, loss: 0 })).toBe("0%");
    expect(nodeLossText({ ...node, loss: 0.03 })).toBe("3%");
  });

  it("reads a node with no loss figure as not reported", () => {
    expect(nodeLossText(node)).toBe("not reported");
  });

  it("names the hops the figure composes in its hint", () => {
    expect(nodeLossHint(view())).toMatch(/node hop/);
    expect(nodeLossHint(view({ loss_hops: ["node", "sim"] }))).toMatch(/node and sim hops/);
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
