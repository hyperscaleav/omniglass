import { describe, it, expect } from "vitest";
import {
  deviceStateText,
  devicePlaceText,
  filterDevices,
  isSectionOpen,
  outpostsSummary,
  pageCount,
  pageSlice,
  pipelineSummary,
  serverSummary,
  setSectionOpen,
  simulationSummary,
} from "./platform_sections";
import type { PlatformPipeline, PlatformServer, PlatformSimDevice, PlatformSimGap, PlatformSimOutpost, PlatformView } from "./platformhealth";

// The summary line each Platform health section keeps while it is closed (#2446):
// a verdict and the one number that matters, derived from the view the page
// already holds, so the first screen answers "is the platform healthy, and if
// not, where" without opening anything.

const fig = (inn: number, out: number) => ({ in: inn, out, ...(inn > 0 ? { loss: (inn - out) / inn } : {}) });
const span = (inn: number, out: number) => ({ newest: fig(inn, out), last_hour: fig(inn, out) });
const hop = (id: string, label: string, inn: number, out: number) => ({
  id, label, description: "", component_class: "outpost", keys: [], kinds: [{ kind: "metric" as const, ...span(inn, out) }], total: span(inn, out), by_component: [],
});
const pipeline = (hops: ReturnType<typeof hop>[], endLoss: number | undefined): PlatformPipeline => ({
  keys: [], hops, end_to_end: [], end_to_end_total: { newest: endLoss, last_hour: endLoss },
}) as PlatformPipeline;

describe("pipelineSummary", () => {
  it("reads a loss-free path as healthy with the end to end figure", () => {
    expect(pipelineSummary(pipeline([hop("outpost", "Outpost", 100, 100)], 0))).toEqual({ tone: "ok", text: "0% lost end to end" });
  });
  it("names the worst hop when something is lost", () => {
    const p = pipeline([hop("outpost", "Outpost", 100, 99), hop("ingest", "Ingest", 100, 90)], 0.109);
    expect(pipelineSummary(p)).toEqual({ tone: "bad", text: "11% lost end to end, worst at Ingest (10%)" });
  });
  it("says there are no figures yet rather than a fake 0", () => {
    expect(pipelineSummary(pipeline([], undefined))).toEqual({ tone: "none", text: "no figures yet" });
    expect(pipelineSummary(undefined)).toEqual({ tone: "none", text: "no figures yet" });
  });
});

const server = (alarms: { key?: string; message?: string }[], over: Partial<PlatformServer> = {}): PlatformServer => ({
  component: "c1", name: "server", health: "healthy",
  alarms: alarms.map((a) => ({ rule: "r", key: a.key, severity: "warning", message: a.message ?? "m", raised_at: "2026-11-09T12:00:00Z" })),
  groups: [
    { name: "requests", label: "Requests", keys: [{ name: "http-request-duration-max", label: "Slowest", unit: "ms", description: "" }] },
    { name: "ingest", label: "Ingest", keys: [{ name: "ingest-lag-max", label: "Lag", unit: "s", description: "" }] },
  ],
  ...over,
}) as PlatformServer;

describe("serverSummary", () => {
  it("reads a server with no open alarm as quiet", () => {
    expect(serverSummary(server([]))).toEqual({ tone: "ok", text: "no open alarms" });
  });
  it("counts the open alarms and names the panels they flag", () => {
    expect(serverSummary(server([{ key: "ingest-lag-max" }]))).toEqual({ tone: "warn", text: "1 open alarm, Ingest flagged" });
    expect(serverSummary(server([{ key: "ingest-lag-max" }, { key: "http-request-duration-max" }, { key: "ingest-lag-max" }]))).toEqual({
      tone: "warn", text: "3 open alarms, Requests and Ingest flagged",
    });
  });
  it("counts an alarm raised on no panel key without naming a panel", () => {
    expect(serverSummary(server([{ key: undefined }]))).toEqual({ tone: "warn", text: "1 open alarm" });
  });
});

const outpost = (name: string, last: string | null, health = "healthy") =>
  ({ name, label: "", enrolled: true, last_heartbeat_at: last ?? undefined, health, figures: {} }) as unknown as NonNullable<PlatformView["outposts"]>[number];
const viewOf = (outposts: PlatformView["outposts"]): PlatformView =>
  ({ read_at: "2026-11-09T12:00:00Z", lab: false, figures_read: true, servers: [], groups: [], outposts, loss_hops: [] }) as unknown as PlatformView;

describe("outpostsSummary", () => {
  it("counts the outposts checking in", () => {
    const v = viewOf([outpost("a", "2026-11-09T11:59:50Z"), outpost("b", "2026-11-09T11:59:55Z")]);
    expect(outpostsSummary(v)).toEqual({ tone: "ok", text: "2 of 2 up" });
  });
  it("says how many are not up", () => {
    const v = viewOf([outpost("a", "2026-11-09T11:59:50Z"), outpost("b", "2026-11-09T10:00:00Z"), outpost("c", null)]);
    expect(outpostsSummary(v)).toEqual({ tone: "bad", text: "1 of 3 up, 2 not up" });
  });
  it("reads no outposts as none", () => {
    expect(outpostsSummary(viewOf([]))).toEqual({ tone: "none", text: "no outposts yet" });
  });
});

const gap = (over: Partial<PlatformSimGap>): PlatformSimGap => ({
  gap: "sent-not-received", protocol: "http", count: 0, in: 100, ratio: 0, masked: 0, masked_by: "", answered_late: 0, reading: "loss", ...over,
});
const simOutpost = (over: Partial<PlatformSimOutpost>): PlatformSimOutpost => ({
  outpost: "edge", state: "measured", start: null, end: null, brackets: 3, error: "", gaps: [gap({})], receipts: { landed: 1, margin: 0, backlog: 0, after_end: 0 },
  protocols: [], in_flight_high: 0, queue_depth: null, busy_workers: null, devices: [], ...over,
}) as PlatformSimOutpost;

describe("simulationSummary", () => {
  it("reads a measured window with every gap at 0 as no loss, with the device count", () => {
    const d = (n: string) => ({ component_id: n, component: n, received: {}, impaired: false, unhit: false, system: "", place: [] });
    expect(simulationSummary([simOutpost({ devices: [d("a"), d("b")] })])).toEqual({ tone: "ok", text: "no loss, 2 devices" });
    expect(simulationSummary([simOutpost({ devices: [d("a")] })])).toEqual({ tone: "ok", text: "no loss, 1 device" });
  });
  it("names the worst unexplained gap", () => {
    const s = simOutpost({ gaps: [gap({}), gap({ gap: "published-not-landed", protocol: "", count: 1, in: 12, ratio: 1 / 12 })] });
    expect(simulationSummary([s])).toEqual({ tone: "bad", text: "losing at published, not landed (8.3%)" });
  });
  it("does not call an impaired device's expected loss a fault", () => {
    const s = simOutpost({ gaps: [gap({ ratio: 0.5, reading: "impaired" })] });
    expect(simulationSummary([s])).toEqual({ tone: "ok", text: "no loss beyond impairments, 0 devices" });
  });
  it("reads a window with no figures yet as warming up or restarted", () => {
    expect(simulationSummary([simOutpost({ state: "warming up", gaps: null })])).toEqual({ tone: "none", text: "warming up" });
    expect(simulationSummary([simOutpost({ state: "restarted" })])).toEqual({ tone: "none", text: "window restarted" });
  });
  it("reads no outpost with a simulator", () => {
    expect(simulationSummary([])).toEqual({ tone: "none", text: "no simulator checking in yet" });
    expect(simulationSummary(undefined)).toEqual({ tone: "none", text: "reading" });
  });
});

describe("open state held in the URL", () => {
  it("is closed with no parameter", () => {
    expect(isSectionOpen(undefined, "pipeline")).toBe(false);
    expect(isSectionOpen("", "pipeline")).toBe(false);
  });
  it("reads a comma list", () => {
    expect(isSectionOpen("pipeline,outposts", "outposts")).toBe(true);
    expect(isSectionOpen("pipeline,outposts", "simulation")).toBe(false);
    expect(isSectionOpen(["pipeline", "outposts"] as unknown as string, "x")).toBe(false);
  });
  it("opens one section at a time, in a stable order, and drops the parameter when none is open", () => {
    expect(setSectionOpen(undefined, "outposts", true)).toBe("outposts");
    expect(setSectionOpen("outposts", "pipeline", true)).toBe("outposts,pipeline");
    expect(setSectionOpen("outposts,pipeline", "outposts", true)).toBe("outposts,pipeline");
    expect(setSectionOpen("outposts,pipeline", "outposts", false)).toBe("pipeline");
    expect(setSectionOpen("pipeline", "pipeline", false)).toBeUndefined();
  });
});

const dev = (over: Partial<PlatformSimDevice>): PlatformSimDevice => ({ component_id: "c", component: "amp-1", received: {}, impaired: false, unhit: false, system: "", place: [], ...over });

describe("device table", () => {
  it("tells a repeated name apart by its system and place", () => {
    expect(devicePlaceText(dev({ system: "board-meeting", place: ["hq", "level-2", "boardroom-a"] }))).toBe("board-meeting · hq / level-2 / boardroom-a");
    expect(devicePlaceText(dev({ system: "", place: ["hq"] }))).toBe("hq");
    expect(devicePlaceText(dev({ system: "board", place: [] }))).toBe("board");
    expect(devicePlaceText(dev({}))).toBe("");
  });
  it("fills the state column for every device", () => {
    expect(deviceStateText(dev({}))).toEqual(["answering"]);
    expect(deviceStateText(dev({ impaired: true }))).toEqual(["impaired"]);
    expect(deviceStateText(dev({ unhit: true }))).toEqual(["due, never seen"]);
    expect(deviceStateText(dev({ unhit: true, impaired: true }))).toEqual(["due, never seen", "impaired"]);
  });
  it("filters by name, system or place, case-insensitively, every word", () => {
    const list = [
      dev({ component_id: "1", system: "board-meeting", place: ["hq", "boardroom-a"] }),
      dev({ component_id: "2", system: "auditorium", place: ["east", "auditorium"] }),
      dev({ component_id: "3", component: "camera-1", system: "auditorium", place: ["east"] }),
    ];
    expect(filterDevices(list, "").length).toBe(3);
    expect(filterDevices(list, "  ").length).toBe(3);
    expect(filterDevices(list, "AMP").map((d) => d.component_id)).toEqual(["1", "2"]);
    expect(filterDevices(list, "boardroom").map((d) => d.component_id)).toEqual(["1"]);
    expect(filterDevices(list, "amp east").map((d) => d.component_id)).toEqual(["2"]);
    expect(filterDevices(list, "nothing")).toEqual([]);
  });
  it("pages a list and clamps the page", () => {
    const items = Array.from({ length: 5 }, (_, i) => i);
    expect(pageCount(0, 2)).toBe(1);
    expect(pageCount(5, 2)).toBe(3);
    expect(pageSlice(items, 0, 2)).toEqual([0, 1]);
    expect(pageSlice(items, 2, 2)).toEqual([4]);
    expect(pageSlice(items, 9, 2)).toEqual([4]);
    expect(pageSlice(items, -1, 2)).toEqual([0, 1]);
  });
});
