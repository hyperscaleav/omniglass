import { describe, it, expect, afterEach, vi } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Rules, { RULE_HINTS } from "./Rules";
import { EVENT_RULES_KEY, ruleStateKey, type EventRule } from "../lib/event_rules";
import { severityLevelsKey, type SeverityLevel } from "../lib/severity";
import { PROPERTIES_KEY, type PropertyRow } from "../lib/properties";
import { METRICS_KEY, type MetricRow } from "../lib/metric_types";
import { COMPONENT_TYPES_KEY } from "../lib/component_types";
import { COMPONENTS_KEY } from "../lib/components";
import { ME_KEY, type Me } from "../lib/auth";
import { OFFICIAL_LOCK } from "../lib/catalog";
import { uuidFor } from "../lib/testids";

// The Rules page (#921): a FlatList over /event-rules, each row reading its
// criteria in words and its live state (open alarms, pending streaks); a row
// opens a blade with the full rule, its live state and its actions; Edit and
// New open the one RuleForm in a Drawer. Data is seeded into the query cache;
// writes are caught at fetch.

const reach: EventRule = {
  id: uuidFor("rule-reachability-lost"), name: "reachability-lost", label: "Reachability lost",
  selector: { component_types: [], components: [] }, sample_key: "endpoint-reachable", sample_lane: "property",
  fire_criteria: { op: "eq", value: "down" }, clear_criteria: { op: "eq", value: "up" },
  fire_sustain: "15s", clear_sustain: "0s", severity: "high", enabled: true, official: true, version: 1,
};
const hot: EventRule = {
  id: uuidFor("rule-room-too-hot"), name: "room-too-hot", label: "Too hot",
  selector: { component_types: ["display"], components: [] }, sample_key: "temperature-c", sample_lane: "metric",
  fire_criteria: { op: "gt", value: 40 }, clear_criteria: { op: "lt", value: 35 }, fire_sustain: "1m0s", clear_sustain: "30s",
  severity: "average", enabled: false, official: false, version: 2,
};

const levels: SeverityLevel[] = [
  { name: "info", label: "Info", color: "gray", rank: 10, outage: false },
  { name: "average", label: "Average", color: "orange", rank: 30, outage: false },
  { name: "high", label: "High", color: "red", rank: 40, outage: true },
] as SeverityLevel[];
const properties: PropertyRow[] = [{ name: "endpoint-reachable", data_type: "string", label: "Endpoint reachable", official: true }];
const metrics: MetricRow[] = [{ name: "temperature-c", data_type: "float", label: "Temperature", unit: "°C", official: true }];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [{ role: "viewer", scope_kind: "all" }] };
const scoped: Me = { principal: { id: "u-hq", kind: "human" }, human: { username: "viewer-hq" }, permissions: ["*:read"], grants: [{ role: "viewer", scope_kind: "location", scope_id: uuidFor("loc-hq") }] };

const display = uuidFor("comp-display-1");
const reachState = {
  open: [{ alarm_id: uuidFor("alarm-1"), component: "display-1", component_id: display, instance: "icmp", severity: "high", raised_at: new Date(Date.now() - 60_000).toISOString() }],
  pending: [{ component: "codec-1", component_id: uuidFor("comp-codec-1"), instance: "http", pending: "fire" as const, since: new Date(Date.now() - 5_000).toISOString() }],
};

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

function mount(me: Me = admin, rules: EventRule[] = [reach, hot]) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...EVENT_RULES_KEY], rules);
  qc.setQueryData([...ruleStateKey("reachability-lost")], reachState);
  qc.setQueryData([...ruleStateKey("room-too-hot")], { open: [], pending: [] });
  qc.setQueryData([...severityLevelsKey], levels);
  qc.setQueryData([...PROPERTIES_KEY], properties);
  qc.setQueryData([...METRICS_KEY], metrics);
  qc.setQueryData([...COMPONENT_TYPES_KEY], []);
  qc.setQueryData([...COMPONENTS_KEY], []);
  qc.setQueryData([...ME_KEY], me);
  return Object.assign(
    render(() => (
      <QueryClientProvider client={qc}>
        <Router><Route path="*" component={Rules} /></Router>
      </QueryClientProvider>
    )),
    { qc },
  );
}

// Every write lands here; a GET answers from the fixtures.
function catchWrites() {
  const writes: { method: string; url: string; body: unknown }[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const req = input as Request;
    if (req.method !== "GET") {
      const body = req.method === "DELETE" ? undefined : await req.clone().json();
      writes.push({ method: req.method, url: req.url, body });
      return req.method === "DELETE" ? new Response(null, { status: 204 }) : json({ ...hot, ...(body as object) });
    }
    if (req.url.endsWith("/state")) return json({ open: [], pending: [] });
    return json({ event_rules: [reach, hot] });
  });
  return writes;
}

describe("Rules page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists each rule with what it reads, its criteria in words, severity and live counts", () => {
    mount();
    expect(screen.getByText("Reachability lost")).toBeTruthy();
    expect(screen.getByText("endpoint-reachable")).toBeTruthy();
    expect(screen.getByText("reads down for 15s")).toBeTruthy();
    expect(screen.getByText("clears on up")).toBeTruthy();
    expect(screen.getByText("reads above 40 °C for 1m")).toBeTruthy();
    expect(screen.getByText("clears on below 35 °C after 30s")).toBeTruthy();
    const live = document.querySelector('[data-live="reachability-lost"]') as HTMLElement;
    expect(live.textContent).toContain("1 open");
    expect(live.textContent).toContain("1 pending");
    // A disabled rule says so before anything else.
    expect((document.querySelector('[data-live="room-too-hot"]') as HTMLElement).textContent).toContain("disabled");
    expect(document.querySelector('[data-severity="high"]')?.textContent).toBe("High");
  });

  it("shows a rule's open alarms, linked to their components by id, and its pending streaks", async () => {
    mount();
    fireEvent.click(screen.getByText("Reachability lost"));
    const b = await blade();
    const open = within(b).getByRole("region", { name: "Open alarms" });
    const link = within(open).getByText("display-1").closest("a")!;
    expect(link.getAttribute("href")).toBe(`/components/${display}`);
    expect(within(open).getByText("icmp")).toBeTruthy();
    const pending = within(b).getByRole("region", { name: "Pending streaks" });
    expect(within(pending).getByText("codec-1")).toBeTruthy();
    expect(within(pending).getByText("to fire")).toBeTruthy();
    expect(within(pending).getByText(/^held \d+s$/)).toBeTruthy();
    expect(within(b).getByTestId("rule-fire").textContent).toBe("reads down for 15s");
    expect(within(b).getByText("Every component")).toBeTruthy();
    // The level it raises at links to the severity registry beside the rules.
    expect(within(b).getByRole("link", { name: "All levels" }).getAttribute("href")).toBe("/severity-levels");
  });

  it("caps a long live list at eight rows and counts the rest", async () => {
    const { qc } = mount();
    const open = Array.from({ length: 11 }, (_, i) => ({ ...reachState.open[0], alarm_id: uuidFor(`alarm-cap-${i}`) }));
    qc.setQueryData([...ruleStateKey("reachability-lost")], { open, pending: [] });
    fireEvent.click(screen.getByText("Reachability lost"));
    const b = await blade();
    const region = within(b).getByRole("region", { name: "Open alarms" });
    expect(within(region).getAllByRole("listitem")).toHaveLength(8);
    expect(within(region).getByText("and 3 more")).toBeTruthy();
  });

  it("keeps an official rule read-only: the pair greys with the official reason, no toggle", async () => {
    mount();
    fireEvent.click(screen.getByText("Reachability lost"));
    const b = await blade();
    const editBtn = within(b).getByLabelText("Edit") as HTMLButtonElement;
    const deleteBtn = within(b).getByText("Delete").closest("button") as HTMLButtonElement;
    expect(editBtn.disabled).toBe(true);
    expect(deleteBtn.disabled).toBe(true);
    expect(editBtn.closest(".tooltip")?.getAttribute("data-tip")).toBe(OFFICIAL_LOCK);
    expect(within(b).queryByText("Disable")).toBeNull();
  });

  it("enables a disabled custom rule with a PATCH of enabled alone", async () => {
    const writes = catchWrites();
    mount();
    fireEvent.click(screen.getByText("Too hot"));
    const b = await blade();
    fireEvent.click(within(b).getByText("Enable"));
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0].method).toBe("PATCH");
    expect(writes[0].url).toMatch(/\/event-rules\/room-too-hot$/);
    expect(writes[0].body).toEqual({ enabled: true });
  });

  it("gates every affordance on rule permissions", async () => {
    mount(viewer);
    expect(screen.queryByText("New rule")).toBeNull();
    fireEvent.click(screen.getByText("Too hot"));
    const b = await blade();
    expect(within(b).queryByText("Enable")).toBeNull();
    const editBtn = within(b).getByLabelText("Edit") as HTMLButtonElement;
    expect(editBtn.disabled).toBe(true);
    expect(editBtn.closest(".tooltip")?.getAttribute("data-tip")).toBe("Requires rule:update");
  });

  it("tells a scoped caller that rules are fleet-wide instead of looking empty", () => {
    mount(scoped, []);
    expect(screen.getByText(/Rules are fleet-wide config/)).toBeTruthy();
    expect(screen.queryByText("New rule")).toBeNull();
  });

  it("creates a property rule: eq/ne only, text value, the opposite as its clear", async () => {
    const writes = catchWrites();
    mount();
    fireEvent.click(screen.getByText("New rule"));
    const name = await waitFor(() => screen.getByLabelText("Name") as HTMLInputElement);
    fireEvent.input(name, { target: { value: "reach-flap" } });
    fireEvent.change(screen.getByLabelText("Reads"), { target: { value: "property:endpoint-reachable" } });
    const op = screen.getByLabelText("Fire comparison") as HTMLSelectElement;
    expect([...op.options].map((o) => o.value)).toEqual(["eq", "ne"]);
    fireEvent.change(op, { target: { value: "ne" } });
    fireEvent.input(screen.getByLabelText("Fire value"), { target: { value: "up" } });
    fireEvent.input(screen.getByLabelText("For at least"), { target: { value: "45s" } });
    const submit = screen.getByText("Create rule").closest("button") as HTMLButtonElement;
    await waitFor(() => expect(submit.disabled).toBe(false));
    fireEvent.click(submit);
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0].method).toBe("POST");
    expect(writes[0].body).toEqual({
      name: "reach-flap", sample_key: "endpoint-reachable",
      fire_criteria: { op: "ne", value: "up" }, fire_sustain: "45s", clear_sustain: "0s",
      // The default level: the worst that does not take its component out.
      severity: "average", enabled: true, selector: { component_types: [], components: [] },
    });
  });

  it("offers a metric all six comparisons and refuses a value that is no number", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    mount();
    fireEvent.click(screen.getByText("New rule"));
    const name = await waitFor(() => screen.getByLabelText("Name") as HTMLInputElement);
    fireEvent.input(name, { target: { value: "too-hot" } });
    fireEvent.change(screen.getByLabelText("Reads"), { target: { value: "metric:temperature-c" } });
    const op = screen.getByLabelText("Fire comparison") as HTMLSelectElement;
    expect([...op.options].map((o) => o.value)).toEqual(["eq", "ne", "gt", "ge", "lt", "le"]);
    fireEvent.input(screen.getByLabelText("Fire value"), { target: { value: "warm" } });
    expect(screen.getByText(/compares numbers/)).toBeTruthy();
    const submit = screen.getByText("Create rule").closest("button") as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
    fireEvent.input(screen.getByLabelText("For at least"), { target: { value: "ninety" } });
    expect(screen.getByText(/A duration like 30s/)).toBeTruthy();
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("edits a custom rule in a Drawer, dropping its clear criterion with clear_negates", async () => {
    const writes = catchWrites();
    mount();
    fireEvent.click(screen.getByText("Too hot"));
    const b = await blade();
    fireEvent.click(within(b).getByText("Edit"));
    const clears = await waitFor(() => screen.getByLabelText("Clears") as HTMLSelectElement);
    expect(clears.value).toBe("custom");
    // Opening the form closed the blade.
    expect(asides()).toHaveLength(0);
    fireEvent.change(clears, { target: { value: "opposite" } });
    fireEvent.click(screen.getByText("Save rule").closest("button")!);
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0].method).toBe("PATCH");
    const body = writes[0].body as Record<string, unknown>;
    expect(body.clear_negates).toBe(true);
    expect(body.clear_criteria).toBeUndefined();
    expect(body.fire_criteria).toEqual({ op: "gt", value: 40 });
    expect(body.fire_sustain).toBe("1m");
    expect(body.clear_sustain).toBe("30s");
    expect(body.selector).toEqual({ component_types: ["display"], components: [] });
    // Saving lands back on the rule's blade.
    await blade();
  });

  // Disabling clears what the rule holds open (storage settleRuleWrite), and the
  // hint beside Status says so.
  it("teaches that disabling clears the alarms a rule holds open", () => {
    expect(RULE_HINTS.enabled).toMatch(/clears the alarms it holds open/);
    expect(RULE_HINTS.enabled).not.toMatch(/stays open/);
  });

  it("deletes a custom rule from the kebab after a confirmation", async () => {
    const writes = catchWrites();
    const ask = vi.spyOn(window, "confirm").mockReturnValue(true);
    mount();
    fireEvent.click(screen.getByText("Too hot"));
    const b = await blade();
    fireEvent.click(within(b).getByText("Delete"));
    await waitFor(() => expect(writes).toHaveLength(1));
    // The server clears what the rule holds open before it goes (storage
    // DeleteEventRule), so the confirmation says so rather than promising the
    // alarms stay open.
    expect(ask.mock.calls[0][0]).toMatch(/clears the alarms it holds open/);
    expect(writes[0].method).toBe("DELETE");
    expect(writes[0].url).toMatch(/\/event-rules\/room-too-hot$/);
  });
});
