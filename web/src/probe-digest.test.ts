import { describe, it, expect } from "vitest";
import { DEFAULT_BUDGETS, normalizeText, p95, parseRaw, reduce, routeKey, routeMatchers, serialize } from "../e2e/digestlib.mjs";

const TEMPLATES = ["/api/v1/components/{name}/reachability", "/api/v1/components/{name}", "/api/v1/components:search", "/api/v1/locations"];

const id = "0192f3a4-5b6c-7d8e-9f01-23456789abcd";

describe("routeKey", () => {
  it("folds ids and numbers so one route is one group", () => {
    expect(routeKey(`http://srv:8080/api/v1/components/${id}/reachability?x=1`)).toBe("/api/v1/components/:id/reachability");
    expect(routeKey("/api/v1/alarms/42")).toBe("/api/v1/alarms/:n");
  });
  it("takes the API's own template when one matches, names and all", () => {
    const m = routeMatchers(TEMPLATES);
    expect(routeKey("/api/v1/components/display-1/reachability", m)).toBe("/api/v1/components/{name}/reachability");
    expect(routeKey("/api/v1/components/bar-2", m)).toBe("/api/v1/components/{name}");
    expect(routeKey("/api/v1/components:search", m)).toBe("/api/v1/components:search");
  });
});

describe("normalizeText", () => {
  it("folds run-specific parts and bounds the length", () => {
    expect(normalizeText(`row ${id} failed after 1234 ms`, 200)).toBe("row <id> failed after <n> ms");
    expect(normalizeText("x".repeat(300), 10)).toHaveLength(10);
  });
});

describe("p95", () => {
  it("takes the 95th percentile, and zero for nothing", () => {
    expect(p95([])).toBe(0);
    expect(p95(Array.from({ length: 100 }, (_, i) => i + 1))).toBe(95);
  });
});

describe("reduce", () => {
  const visit = (surface: string) => ({ kind: "visit", surface });

  it("a clean run has no findings", () => {
    const d = reduce([visit("route:/explore"), { kind: "response", surface: "route:/explore", method: "GET", url: "/api/v1/locations", status: 200, ms: 40 }]);
    expect(d.findings).toEqual([]);
    expect(d.surfaces).toBe(1);
    expect(d.requests).toBe(1);
    expect(d.slowest).toEqual([{ route: "GET /api/v1/locations", count: 1, p95Ms: 40 }]);
  });

  it("names an injected console error, grouped across rows", () => {
    const d = reduce([
      { kind: "console", surface: "route:/alarms", text: `TypeError: cannot read row ${id}` },
      { kind: "console", surface: "route:/explore", text: "TypeError: cannot read row 0192f3a4-0000-7d8e-9f01-23456789abcd" },
    ]);
    expect(d.findings).toHaveLength(1);
    expect(d.findings[0]).toMatchObject({ kind: "console-error", key: "TypeError: cannot read row <id>", count: 2, surfaces: ["route:/alarms", "route:/explore"] });
  });

  it("names an injected slow route once, however many devices it was called for", () => {
    const slow = Array.from({ length: 20 }, (_, i) => ({ kind: "response", surface: "route:/fleet", method: "GET", url: `/api/v1/components/display-${i}/reachability`, status: 200, ms: 2400 }));
    const d = reduce([...slow, { kind: "response", surface: "route:/fleet", method: "GET", url: "/api/v1/locations", status: 200, ms: 30 }], {}, TEMPLATES);
    expect(d.findings).toHaveLength(1);
    expect(d.findings[0]).toMatchObject({ kind: "slow-route", key: "GET /api/v1/components/{name}/reachability", count: 20, surfaces: ["route:/fleet"] });
    expect(d.slowest[0].route).toBe("GET /api/v1/components/{name}/reachability");
  });

  it("names the surfaces a run planned and never drove", () => {
    const d = reduce([{ kind: "plan", surface: "probe", surfaces: 3 }, visit("route:/a"), visit("route:/b")]);
    expect(d.findings[0]).toMatchObject({ kind: "probe-incomplete", severity: "high" });
    expect(d.findings[0].detail).toMatch(/^1 of 3 planned/);
    expect(reduce([{ kind: "plan", surface: "probe", surfaces: 2 }, visit("route:/a"), visit("route:/b")]).findings).toEqual([]);
  });

  it("sums an accessibility rule's nodes across surfaces", () => {
    const d = reduce([
      { kind: "axe", surface: "route:/b", id: "color-contrast", impact: "serious", help: "contrast", nodes: 3 },
      { kind: "axe", surface: "route:/a", id: "color-contrast", impact: "serious", help: "contrast", nodes: 4 },
    ]);
    expect(d.findings[0].detail).toBe("serious: contrast (7 nodes across 2 surfaces)");
  });

  it("ranks breakage above friction", () => {
    const d = reduce([
      { kind: "axe", surface: "route:/a", id: "color-contrast", impact: "minor", help: "contrast", nodes: 3 },
      { kind: "response", surface: "route:/a", method: "POST", url: "/api/v1/tags", status: 500, ms: 10 },
      { kind: "walkthrough", surface: "walkthrough:set-up-monitoring", ok: false, step: 4, reason: "never appeared" },
    ]);
    expect(d.findings.map((f) => f.kind)).toEqual(["http-5xx", "walkthrough-failed", "a11y"]);
    expect(d.findings[1].detail).toBe("step 4: never appeared");
  });

  it("flags vitals over budget only", () => {
    const d = reduce([
      { kind: "vitals", surface: "route:/a", lcp: 900, cls: 0.01 },
      { kind: "vitals", surface: "route:/b", lcp: 4000, cls: 0.4 },
    ]);
    expect(d.findings.map((f) => `${f.kind}:${f.key}`).sort()).toEqual(["layout-shift:route:/b", "slow-paint:route:/b"]);
  });

  it("stays under its byte cap however much the run saw, counting what it dropped", () => {
    const flood = Array.from({ length: 2000 }, (_, i) => ({ kind: "pageerror", surface: `route:/r${i}`, text: `distinct failure ${"abcdefghij".repeat(15)} ${String.fromCharCode(97 + (i % 26))}${i.toString(36)}` }));
    const d = reduce(flood);
    expect(new TextEncoder().encode(serialize(d)).length).toBeLessThanOrEqual(DEFAULT_BUDGETS.maxBytes);
    expect(d.findings.length + d.truncated).toBe(2000);
    expect(d.truncated).toBeGreaterThan(0);
  });

  it("depends on which events arrived, never their order", () => {
    const events = [
      { kind: "console", surface: "route:/b", text: "b" },
      { kind: "console", surface: "route:/a", text: "a" },
      ...Array.from({ length: 8 }, (_, i) => ({ kind: "axe", surface: `route:/r${i}`, id: "label", impact: "critical", help: "labels", nodes: i + 1 })),
      ...Array.from({ length: 8 }, (_, i) => ({ kind: "console", surface: `route:/s${i}`, text: `shared fault ${i % 2 ? "x" : "y"}` })),
    ];
    expect(reduce(events)).toEqual(reduce([...events].reverse()));
  });
});

describe("parseRaw", () => {
  it("keeps an unparseable line as a finding rather than dropping it", () => {
    const events = parseRaw('{"kind":"visit","surface":"route:/a"}\nnot json\n\n');
    expect(events).toHaveLength(2);
    expect(reduce(events).findings[0].kind).toBe("console-error");
  });
});
