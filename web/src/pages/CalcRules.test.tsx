import { describe, it, expect, afterEach, vi } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import CalcRules from "./CalcRules";
import { CALC_RULES_KEY, calcStateKey, type CalcRule, type CalcRuleState } from "../lib/calc_rules";
import { PROPERTIES_KEY, type PropertyRow } from "../lib/properties";
import { METRICS_KEY, type MetricRow } from "../lib/metric_types";
import { COMPONENT_TYPES_KEY } from "../lib/component_types";
import { COMPONENTS_KEY } from "../lib/components";
import { SYSTEM_TYPES_KEY } from "../lib/system_types";
import { SYSTEMS_KEY } from "../lib/systems";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";
import { typeExpression } from "../components/ExpressionField.testing";

// The Calc rules page (#999): a FlatList over /calc-rules, each row reading its
// owner kind, inputs, reduce, output and live counts; a row opens a blade with
// the rule and its state per owner; New and Edit open the one CalcRuleForm in a
// Drawer, with its preview. Data is seeded into the query cache; writes are
// caught at fetch.

const mean: CalcRule = {
  id: uuidFor("calc-room-mean"), name: "room-mean", label: "Room mean", owner_kind: "system",
  selector: { system_types: ["av"] }, inputs: [{ as: "t", key: "room-temperature" }], reduce: { reducer: "average" },
  output_key: "avg-temp", output_lane: "metric", enabled: true, official: false, version: 2,
  created_at: "2026-10-21T00:00:00Z", updated_at: "2026-10-21T00:00:00Z",
  owners_computed: 2, owners_pending: 1, owners_unsupported: 1, owners_failed: 0,
};
const busy: CalcRule = {
  id: uuidFor("calc-busy"), name: "busy", label: "Busy", owner_kind: "component",
  selector: { component_types: [], components: [] }, inputs: [{ as: "call", key: "in-call" }, { as: "n", key: "occupancy-count" }],
  reduce: { expr: "call || n > 0" }, output_key: "busy-flag", output_lane: "property", enabled: false, official: false, version: 1,
  created_at: "2026-10-21T00:00:00Z", updated_at: "2026-10-21T00:00:00Z",
  owners_computed: 0, owners_pending: 0, owners_unsupported: 0, owners_failed: 0,
  calc_error: { field: "reduce.expr", line: 1, column: 6, message: "unknown name call" },
};
const sysA = uuidFor("sys-a");
const state: CalcRuleState = {
  pending_depth: 1,
  owners: [
    { owner_kind: "system", owner: "sys-a", owner_id: sysA, value: 21.5, computed_at: new Date(Date.now() - 30_000).toISOString(),
      rule_version: 2, missing: [], skipped: [{ key: "room-temperature", component: "mic-1", component_id: uuidFor("mic-1") }], skipped_hidden: 2,
      stale: [], stale_hidden: 0, pending: true },
    { owner_kind: "system", owner: "sys-b", owner_id: uuidFor("sys-b"), rule_version: 2, missing: ["room-temperature"], skipped: [], skipped_hidden: 0,
      stale: [], stale_hidden: 0, pending: false, no_members_since: new Date(Date.now() - 3600_000).toISOString(), computed_at: new Date().toISOString() },
  ],
};

const properties: PropertyRow[] = [
  { name: "in-call", data_type: "bool", label: "In call", official: false },
  { name: "busy-flag", data_type: "bool", label: "Busy", official: false },
];
const metrics: MetricRow[] = [
  { name: "room-temperature", data_type: "float", label: "Room temperature", unit: "C", official: true },
  { name: "occupancy-count", data_type: "int", label: "Occupancy", official: true },
  { name: "avg-temp", data_type: "float", label: "Average temperature", official: false },
];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], fleet_permissions: [">"], grants: [{ role: "owner", scope_kind: "all" }] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], fleet_permissions: ["*:read"], grants: [{ role: "viewer", scope_kind: "all" }] };

const asides = () => document.querySelectorAll("aside[data-blade]");
const blade = () =>
  waitFor(() => {
    const el = asides()[0];
    if (!el) throw new Error("no blade yet");
    return el as HTMLElement;
  });

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function mount(me: Me = admin, rules: CalcRule[] = [mean, busy]) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...CALC_RULES_KEY], rules);
  qc.setQueryData([...calcStateKey("room-mean")], state);
  qc.setQueryData([...calcStateKey("busy")], { owners: [], pending_depth: 0 });
  qc.setQueryData([...PROPERTIES_KEY], properties);
  qc.setQueryData([...METRICS_KEY], metrics);
  qc.setQueryData([...COMPONENT_TYPES_KEY], []);
  qc.setQueryData([...COMPONENTS_KEY], []);
  qc.setQueryData([...SYSTEM_TYPES_KEY], [{ id: uuidFor("st-av"), name: "av", label: "AV", official: true }]);
  qc.setQueryData([...SYSTEMS_KEY], []);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router><Route path="*" component={CalcRules} /></Router>
    </QueryClientProvider>
  ));
}

// Answers the write, validate and preview routes, recording every request.
function routes(preview: unknown = { owners: [], owners_judged: 0, owners_total: 0, owner_cap: 200, truncated: false }) {
  const reqs: { method: string; url: string; body: unknown }[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const req = input as Request;
    const body = req.method === "GET" || req.method === "DELETE" ? undefined : await req.clone().json();
    reqs.push({ method: req.method, url: req.url, body });
    if (req.url.endsWith("/expressions:validate")) {
      return json({ ok: true, site: "calc", value_type: "bool", result_type: "bool", diagnostics: [],
        bindings: [{ name: "call", type: "bool" }], builtins: ["abs"], budget: { iterations: 100000, memory: 50000, deadline: "25ms" } });
    }
    if (req.url.endsWith("/calc-rules:preview")) return json(preview);
    if (req.url.endsWith("/state")) return json({ owners: [], pending_depth: 0 });
    if (req.method === "POST" || req.method === "PATCH") return json({ ...mean, ...(body as object) });
    return json({ calc_rules: [mean, busy] });
  });
  return reqs;
}

describe("Calc rules page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists each rule with its owner kind, inputs, reduce, output and live counts", () => {
    routes();
    mount();
    expect(screen.getByText("Room mean")).toBeTruthy();
    expect(screen.getByText("the average of room-temperature")).toBeTruthy();
    expect(screen.getByText("avg-temp")).toBeTruthy();
    const live = document.querySelector('[data-live="room-mean"]') as HTMLElement;
    expect(live.textContent).toContain("2 computed");
    expect(live.textContent).toContain("1 pending");
    expect(live.textContent).toContain("1 without inputs");
    expect((document.querySelector('[data-live="busy"]') as HTMLElement).textContent).toContain("disabled");
    expect(document.querySelector('[data-calc-error="busy"]')?.textContent).toContain("does not compile");
  });

  it("shows a rule's state per owner, naming what it may and counting what it may not", async () => {
    routes();
    mount();
    fireEvent.click(screen.getByText("Room mean"));
    const b = await blade();
    const region = within(b).getByRole("region", { name: "State" });
    const a = within(region).getByText("sys-a").closest("a")!;
    expect(a.getAttribute("href")).toBe(`/systems/${sysA}`);
    expect(within(region).getByText("21.5")).toBeTruthy();
    expect(within(region).getByText(/mic-1/)).toBeTruthy();
    expect(within(region).getByText(/2 members you cannot read/)).toBeTruthy();
    expect(within(region).getByText("pending")).toBeTruthy();
    expect(within(region).getByText(/no members/i)).toBeTruthy();
    expect(within(b).getByText("the average of room-temperature")).toBeTruthy();
  });

  it("alerts a rule whose stored reduce does not compile", async () => {
    routes();
    mount();
    fireEvent.click(screen.getByText("Busy"));
    const b = await blade();
    const alert = within(b).getByTestId("calc-error");
    expect(alert.textContent).toContain("line 1, column 6");
    expect(alert.textContent).toContain("unknown name call");
    expect(within(b).getByTestId("calc-expr").textContent).toBe("call || n > 0");
  });

  it("offers New rule only on an all-scope grant", () => {
    routes();
    mount(viewer);
    expect(screen.queryByText("New rule")).toBeNull();
  });

  it("creates a component rule with a named reducer, offering only the reducers its input takes", async () => {
    const reqs = routes();
    mount();
    fireEvent.click(screen.getByText("New rule"));
    const name = await waitFor(() => screen.getByLabelText("Name") as HTMLInputElement);
    fireEvent.input(name, { target: { value: "temp-peak" } });
    fireEvent.change(screen.getByLabelText("Input 1 key"), { target: { value: "room-temperature" } });
    fireEvent.input(screen.getByLabelText("Input 1 name"), { target: { value: "t" } });
    const reducer = screen.getByLabelText("Reducer") as HTMLSelectElement;
    expect([...reducer.options].map((o) => o.value).filter(Boolean)).toEqual(["average", "min", "max", "sum", "count"]);
    fireEvent.change(reducer, { target: { value: "max" } });
    fireEvent.change(screen.getByLabelText("Writes"), { target: { value: "avg-temp" } });
    const submit = screen.getByText("Create rule").closest("button") as HTMLButtonElement;
    await waitFor(() => expect(submit.disabled).toBe(false));
    fireEvent.click(submit);
    await waitFor(() => expect(reqs.filter((r) => r.method === "POST" && r.url.endsWith("/calc-rules"))).toHaveLength(1));
    expect(reqs.find((r) => r.method === "POST" && r.url.endsWith("/calc-rules"))!.body).toEqual({
      name: "temp-peak", owner_kind: "component", selector: { component_types: [], components: [] },
      inputs: [{ as: "t", key: "room-temperature" }], reduce: { reducer: "max" }, output_key: "avg-temp", enabled: true,
    });
  });

  it("checks an expression reduce against the calc site with the inputs, and previews the draft", async () => {
    const reqs = routes({
      owners: [{ owner_kind: "component", owner: "codec-1", owner_id: uuidFor("codec-1"), value: true, differs: true, missing: [], skipped: [], skipped_hidden: 0, stale: [], stale_hidden: 0, no_members: false }],
      owners_judged: 1, owners_total: 3, owner_cap: 1, truncated: true,
    });
    mount();
    fireEvent.click(screen.getByText("New rule"));
    fireEvent.input(await waitFor(() => screen.getByLabelText("Name")), { target: { value: "in-use" } });
    fireEvent.change(screen.getByLabelText("Input 1 key"), { target: { value: "in-call" } });
    fireEvent.input(screen.getByLabelText("Input 1 name"), { target: { value: "call" } });
    fireEvent.click(screen.getByLabelText("Write the reduce as an expression"));
    fireEvent.change(screen.getByLabelText("Writes"), { target: { value: "busy-flag" } });
    typeExpression(await screen.findByLabelText("Reduce expression"), "call");
    // The field may first ask for the site's vocabulary with an empty source;
    // the check that matters is the one for what was typed.
    const typed = () => reqs.find((r) => r.url.endsWith("/expressions:validate") && (r.body as { source?: string }).source === "call");
    await waitFor(() => expect(typed()).toBeTruthy(), { timeout: 2000 });
    const v = typed()!.body as Record<string, unknown>;
    expect(v).toMatchObject({ site: "calc", source: "call", key: "busy-flag", owner_kind: "component", inputs: [{ as: "call", key: "in-call" }] });
    const run = screen.getByRole("button", { name: "Preview" });
    await waitFor(() => expect((run as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(run);
    const result = await screen.findByTestId("calc-preview-result");
    expect(within(result).getByText("codec-1")).toBeTruthy();
    expect(within(result).getByText("would write")).toBeTruthy();
    expect(within(result).getByTestId("calc-preview-truncated").textContent).toContain("1 of 3");
    expect(reqs.some((r) => r.method === "POST" && r.url.endsWith("/calc-rules"))).toBe(false);
  });

  it("edits a rule, renaming it with its own method after the patch", async () => {
    const reqs = routes();
    mount();
    fireEvent.click(screen.getByText("Room mean"));
    await blade();
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    const name = await waitFor(() => screen.getByLabelText("Name") as HTMLInputElement);
    expect((screen.getByLabelText("Derives on") as HTMLSelectElement).disabled).toBe(true);
    fireEvent.input(name, { target: { value: "room-avg" } });
    fireEvent.input(screen.getByLabelText("Label"), { target: { value: "Room average" } });
    const submit = screen.getByText("Save rule").closest("button") as HTMLButtonElement;
    await waitFor(() => expect(submit.disabled).toBe(false));
    fireEvent.click(submit);
    await waitFor(() => expect(reqs.some((r) => r.url.endsWith("/calc-rules/room-mean:rename"))).toBe(true));
    const writes = reqs.filter((r) => r.method !== "GET");
    expect(writes[0]).toMatchObject({ method: "PATCH", body: { label: "Room average" } });
    expect(writes[1]).toMatchObject({ method: "POST", body: { name: "room-avg" } });
  });
});
