import { describe, it, expect } from "vitest";
import {
  figureText,
  hopFigure,
  lossPercent,
  lossTinted,
  nodeLossHint,
  nodeLossText,
  nodePill,
  pipelineKinds,
  endToEndFigure,
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
