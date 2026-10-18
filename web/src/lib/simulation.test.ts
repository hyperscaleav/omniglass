import { describe, it, expect } from "vitest";
import {
  draftOf,
  entityNameError,
  fireBody,
  fireDraftFor,
  fireReady,
  HEALTHY,
  hitsByProtocol,
  impairmentSummary,
  isImpaired,
  parseImpairment,
  productGroups,
  risenHits,
  sidebarPollMs,
  SIDEBAR_POLL_MS,
  SIDEBAR_IDLE_POLL_MS,
  surfaceKind,
  validateAddComponent,
  validateAddSystem,
  addSystemBody,
  planShortfall,
  simStatus,
  type SimComponent,
  type SimSurface,
} from "./simulation";

const row = (id: string, hits: number): SimComponent => ({
  component_id: id, component: id, product: "p", address: "127.60.0.2", serving: true, status: "running", hits, hits_by_protocol: {}, impaired: false,
});

describe("risenHits", () => {
  it("seeds a component's first read without flashing it", () => {
    const seen = new Map<string, number>();
    expect(risenHits(seen, [row("a", 4)])).toEqual([]);
    expect(seen.get("a")).toBe(4);
  });

  it("flags exactly the components whose count rose since the last read", () => {
    const seen = new Map([["a", 4], ["b", 9]]);
    expect(risenHits(seen, [row("a", 5), row("b", 9), row("c", 1)])).toEqual(["a"]);
    expect(risenHits(seen, [row("a", 5), row("b", 10), row("c", 2)])).toEqual(["b", "c"]);
  });

  it("does not flag a count that fell (a server restart resets hits)", () => {
    const seen = new Map([["a", 40]]);
    expect(risenHits(seen, [row("a", 2)])).toEqual([]);
    expect(seen.get("a")).toBe(2);
  });
});

describe("parseImpairment", () => {
  it("returns the full body, empty fields reading as zero", () => {
    const r = parseImpairment({ unplugged: false, latency_ms: "120", jitter_ms: "", loss_pct: "2.555", rate_kbit: "" });
    expect(r.errors).toEqual({});
    expect(r.value).toEqual({ unplugged: false, latency_ms: 120, jitter_ms: 0, loss_pct: 2.56, rate_kbit: 0 });
  });

  it("refuses jitter above latency, out-of-range values and non-numbers", () => {
    expect(parseImpairment({ ...draftOf(HEALTHY), latency_ms: "10", jitter_ms: "20" }).errors.jitter_ms).toBe("At most the latency.");
    expect(parseImpairment({ ...draftOf(HEALTHY), latency_ms: "10001" }).errors.latency_ms).toMatch(/At most 10,000/);
    expect(parseImpairment({ ...draftOf(HEALTHY), loss_pct: "100.5" }).errors.loss_pct).toBe("At most 100%.");
    expect(parseImpairment({ ...draftOf(HEALTHY), rate_kbit: "1.5" }).errors.rate_kbit).toBe("A whole number.");
    expect(parseImpairment({ ...draftOf(HEALTHY), loss_pct: "-1" }).value).toBeNull();
  });
});

describe("impairment words", () => {
  it("reads healthy, unplugged, or what is degraded", () => {
    expect(isImpaired(HEALTHY)).toBe(false);
    expect(impairmentSummary(undefined)).toBe("healthy");
    expect(impairmentSummary({ unplugged: true, latency_ms: 50 })).toBe("unplugged");
    expect(impairmentSummary({ latency_ms: 200, jitter_ms: 20, loss_pct: 5 })).toBe("200 ms ±20, 5% loss");
    expect(isImpaired({ rate_kbit: 64 })).toBe(true);
  });
});

// The status is the server's state, untouched by an impairment; the impairment
// rides beside it as its own fact, with whether the simulator applied it.
describe("a simulated component's status", () => {
  const sim = (status: SimComponent["status"], impairment?: SimComponent["impairment"], impaired = false) => ({ status, impairment, impaired });

  it("is each state as the server names it, with no impairment when none is set", () => {
    for (const s of ["offline", "failed", "starting", "running"] as const) {
      expect(simStatus(sim(s))).toEqual({ state: s });
    }
  });

  it("keeps offline, failed and starting over any impairment set: the impairment rides beside, never replaces", () => {
    for (const s of ["offline", "failed", "starting"] as const) {
      expect(simStatus(sim(s, { unplugged: true })).state).toBe(s);
    }
    expect(simStatus(sim("offline", { latency_ms: 150 }))).toEqual({
      state: "offline",
      impairment: { unplugged: false, summary: "150 ms", applied: false },
    });
  });

  it("names an applied unplug as unplugged, over any shaping set with it", () => {
    expect(simStatus(sim("running", { unplugged: true, latency_ms: 50 }, true))).toEqual({
      state: "running",
      impairment: { unplugged: true, summary: "unplugged", applied: true },
    });
  });

  it("summarises applied shaping in a few words", () => {
    expect(simStatus(sim("running", { latency_ms: 150, loss_pct: 2.5 }, true))).toEqual({
      state: "running",
      impairment: { unplugged: false, summary: "150 ms, 2.5% loss", applied: true },
    });
  });

  it("marks an impairment the simulator did not apply, and keeps the state", () => {
    expect(simStatus(sim("running", { rate_kbit: 64 }, false))).toEqual({
      state: "running",
      impairment: { unplugged: false, summary: "64 kbit/s", applied: false },
    });
  });

  it("treats an all-zero impairment as none, whatever impaired says", () => {
    expect(simStatus(sim("running", HEALTHY, true))).toEqual({ state: "running" });
  });
});

describe("the add-component rule", () => {
  it("requires a node, a product and a well-formed name", () => {
    expect(validateAddComponent({ node: "", product: "", name: "", location: "" })).toEqual({
      node: expect.any(String), product: expect.any(String), name: "Name the new component.",
    });
    expect(validateAddComponent({ node: "edge-hq", product: "boreal-edge-55", name: "lab-display", location: "" })).toEqual({});
    expect(entityNameError("Lab.Display")).toMatch(/Lowercase/);
    expect(entityNameError("x".repeat(101))).toBe("At most 100 characters.");
  });
});

const http: SimSurface = { name: "web-admin", protocol: "http", port: 10080, fireable: true, routes: [{ method: "POST", path: "/api/audio/mute", example: "{\"mute\": true}\n" }] };
const snmp: SimSurface = { name: "snmp", protocol: "snmp", port: 10161, fireable: true, oids: [{ oid: "1.3.6.1.2.1.1.3.0", path: "status.device.uptime" }] };
const ssh: SimSurface = { name: "ssh", protocol: "ssh", port: 10022, fireable: true, commands: [{ match: "power on", example: "power on" }] };
const icmp: SimSurface = { name: "icmp", protocol: "icmp", port: 0, fireable: false };

describe("firing", () => {
  it("picks the fire form from the protocol", () => {
    expect([http, snmp, ssh, icmp].map(surfaceKind)).toEqual(["http", "snmp", "command", "none"]);
  });

  it("pre-fills from the chosen operation and sends only what its kind reads", () => {
    const d = fireDraftFor(http, { route: http.routes![0] });
    expect(fireBody(d)).toEqual({ surface: "web-admin", method: "POST", path: "/api/audio/mute", body: "{\"mute\": true}" });
    expect(fireBody(fireDraftFor(snmp, { oid: snmp.oids![0] }))).toEqual({ surface: "snmp", oid: "1.3.6.1.2.1.1.3.0" });
    expect(fireBody(fireDraftFor(ssh, { command: ssh.commands![0] }))).toEqual({ surface: "ssh", command: "power on" });
    expect(fireReady(fireDraftFor(icmp))).toBe(false);
  });
});

describe("catalog and counts", () => {
  it("groups products by profile and orders protocols busiest first", () => {
    expect(productGroups([{ name: "x", manufacturer: "Boreal", model: "Edge 86", device_type: "display", summary: "", products: ["a", "b"], protocols: [] }]))
      .toEqual([{ label: "Boreal Edge 86 (display)", products: ["a", "b"] }]);
    expect(hitsByProtocol({ ...row("a", 5), hits_by_protocol: { http: 1, snmp: 4 } }).map((p) => p.protocol)).toEqual(["snmp", "http"]);
  });
});

describe("the sidebar's poll", () => {
  it("asks every SIDEBAR_POLL_MS while a simulator is connected, and backs off otherwise", () => {
    expect(sidebarPollMs({ connected: true, nodes: [] })).toBe(SIDEBAR_POLL_MS);
    expect(sidebarPollMs({ connected: false, nodes: [] })).toBe(SIDEBAR_IDLE_POLL_MS);
    expect(sidebarPollMs(undefined)).toBe(SIDEBAR_IDLE_POLL_MS);
    expect(SIDEBAR_IDLE_POLL_MS).toBeGreaterThan(SIDEBAR_POLL_MS);
  });
});

describe("validateAddSystem and addSystemBody", () => {
  const d = { node: "edge-hq", standard: "meeting-room", system_type: "room-system", name: "", location: "" };
  it("asks for node, standard and type, and lets the name be empty", () => {
    expect(validateAddSystem(d)).toEqual({});
    expect(Object.keys(validateAddSystem({ ...d, node: "", standard: "", system_type: "" })).sort()).toEqual(["node", "standard", "system_type"]);
  });
  it("holds a typed name to the entity name rule", () => {
    expect(validateAddSystem({ ...d, name: "Lab Room" }).name).toBeTruthy();
    expect(validateAddSystem({ ...d, name: "lab-room" }).name).toBeUndefined();
  });
  it("sends only what was filled", () => {
    expect(addSystemBody(d)).toEqual({ standard: "meeting-room", system_type: "room-system" });
    expect(addSystemBody({ ...d, name: " lab-room ", location: "hq" })).toEqual({ standard: "meeting-room", system_type: "room-system", name: "lab-room", location: "hq" });
  });
});

describe("planShortfall", () => {
  it("lists the roles nothing simulated fits and how many they are short", () => {
    const s = planShortfall({ standard: "s", alternates: {}, components: 1, roles: [
      { role: "display", product: "p", count: 1 },
      { role: "camera", count: 0, missing: 2, reason: "none" },
    ] });
    expect(s.roles.map((r) => r.role)).toEqual(["camera"]);
    expect(s.missing).toBe(2);
    expect(planShortfall(undefined)).toEqual({ roles: [], missing: 0 });
  });
});
