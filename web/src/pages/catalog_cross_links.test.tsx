import "@testing-library/jest-dom/vitest";
import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor, within, cleanup } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import type { Component } from "solid-js";
import Products from "./Products";
import Drivers from "./Drivers";
import Vendors from "./Vendors";
import Metrics from "./Metrics";
import Properties from "./Properties";
import ComponentTypes from "./ComponentTypes";
import Standards from "./Standards";
import CommandTypes from "./CommandTypes";
import SeverityLevels from "./SeverityLevels";
import Notifications from "./Notifications";
import Rules from "./Rules";
import CalcRules from "./CalcRules";
import ActionRules from "./ActionRules";
import NotificationTargets from "./NotificationTargets";
import { PRODUCTS_KEY, type Product } from "../lib/products";
import { DRIVERS_KEY, type Driver } from "../lib/drivers";
import { VENDORS_KEY, type Vendor } from "../lib/vendors";
import { METRICS_KEY, type MetricRow } from "../lib/metric_types";
import { PROPERTIES_KEY, type PropertyRow } from "../lib/properties";
import { COMPONENT_TYPES_KEY, type ComponentType } from "../lib/component_types";
import { STANDARDS_KEY, type Standard } from "../lib/standards";
import { COMMAND_TYPES_KEY, type CommandTypeRow } from "../lib/command_types";
import { severityLevelsKey } from "../lib/severity";
import { NOTIFICATION_TYPES_KEY } from "../lib/notification_types";
import { EVENT_RULES_KEY, type EventRule } from "../lib/event_rules";
import { CALC_RULES_KEY, type CalcRule } from "../lib/calc_rules";
import { ACTION_RULES_KEY, type ActionRule } from "../lib/action_rules";
import { NOTIFICATION_TARGETS_KEY, type NotificationTarget } from "../lib/notification_targets";
import { componentUsageKey, systemUsageKey, declaringKey } from "../lib/fleet_usage";
import { classifierPropertiesKey } from "../lib/classifier_properties";
import { classifierMetricsKey } from "../lib/classifier_metrics";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// The catalog can be followed (#2430): every reference a catalog drawer renders is
// a link to the row it names, at its address, and the drawer says what uses the row.
// One test per page, each opening the drawer through its address (?open=) the way a
// link from another page does, then reading the hrefs. A reference rendered as bare
// text fails here, which is the per-page half of the guard (the source half is
// catalog_links_guard.test.ts).

const admin: Me = { user: { id: "u-root", kind: "person" }, person: { username: "root" }, permissions: [">"], grants: [], fleet_permissions: [">"] } as Me;

const vendor: Vendor = { id: uuidFor("v"), name: "boreal", label: "Boreal", kind: "manufacturer", official: true };
const beamDriver: Driver = {
  id: uuidFor("d-beam"), name: "boreal-beam-snmp", label: "Boreal Beam SNMP", official: true, version: "1.0.0",
  spec: {
    version: 1, transport: "snmp",
    polls: [{ name: "status", schedule: { every: "30s" }, request: {}, emits: [{ name: "uptime", extract: {} }, { name: "power-state", extract: {} }] }],
    commands: [{ command_type: "power-on", request: {} }],
  },
};
const beam: Product = {
  id: uuidFor("p-beam"), name: "boreal-beam-340", label: "Boreal Beam 340", kind: "device",
  component_type: "projector", component_type_id: uuidFor("ct-projector"),
  vendor: "boreal", vendor_id: vendor.id, driver: "boreal-beam-snmp", driver_id: beamDriver.id, official: true,
};
const projector: ComponentType = { id: uuidFor("ct-projector"), name: "projector", label: "Projector", official: true, forked: false, default_tags: [] };
const display: ComponentType = { id: uuidFor("ct-display"), name: "display", label: "Display", official: true, forked: false, default_tags: [] };
const projectorChild: ComponentType = { ...projector, id: uuidFor("ct-pj-child"), name: "short-throw", label: "Short throw", parent: "projector", parent_id: projector.id };
const uptime: MetricRow = { name: "uptime", data_type: "float", label: "Uptime", official: true, writer_class: "collected" };
const probe: MetricRow = { name: "tcp-connect-time", data_type: "float", label: "TCP connect time", official: true, writer_class: "outpost-probe" };
const powerState: PropertyRow = { name: "power-state", data_type: "string", label: "Power state", official: true, writer_class: "collected" };
const power: CommandTypeRow = { name: "power-on", label: "Power on", settle_window_seconds: 0, target_property_type: "power-state", official: true };
const eventRule = { id: uuidFor("er"), name: "av-offline", label: "AV offline", sample_key: "uptime", sample_lane: "metric", severity: "high", fire_event_type: "went-offline", fire_criteria: { expr: "value == 0" }, fire_sustain: "0s", clear_sustain: "0s", official: false, enabled: true } as unknown as EventRule;
const calcRule = { id: uuidFor("cr"), name: "uptime-mean", label: "Uptime mean", owner_kind: "component", inputs: [{ alias: "u", key: "uptime" }], reduce: { expr: "avg(u)" }, output_key: "uptime-mean", output_lane: "metric", enabled: true, official: false, version: 1 } as unknown as CalcRule;
const actionRule = { id: uuidFor("ar"), name: "page-ops", label: "Page ops", notification_type: "alarm-page", targets: ["ops-channel"], on: ["raised"], enabled: true, official: false, opt_out_allowed: false } as unknown as ActionRule;
const target = { id: uuidFor("nt"), name: "ops-channel", label: "Ops channel", kind: "user_group", user_group: "help-desk", enabled: true, official: false, channels: ["inbox"] } as unknown as NotificationTarget;
const comp = (n: string) => ({ id: uuidFor(n), name: n, label: n, product: "boreal-beam-340" });

function mount(Page: Component, path: string, seed: (qc: QueryClient) => void) {
  window.history.replaceState({}, "", path);
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], admin);
  seed(qc);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={() => <Page />} />
      </Router>
    </QueryClientProvider>
  ));
}

const blade = async () => (await waitFor(() => {
  const el = document.querySelector("aside[data-blade]");
  if (!el) throw new Error("no blade yet");
  return el;
})) as HTMLElement;

const hrefs = (el: HTMLElement) => [...el.querySelectorAll("a[href]")].map((a) => a.getAttribute("href"));
const seedCatalogs = (qc: QueryClient) => {
  qc.setQueryData([...VENDORS_KEY], [vendor]);
  qc.setQueryData([...DRIVERS_KEY], [beamDriver]);
  qc.setQueryData([...PRODUCTS_KEY], [beam]);
  qc.setQueryData([...COMPONENT_TYPES_KEY], [projector, display, projectorChild]);
  qc.setQueryData([...METRICS_KEY], [uptime, probe]);
  qc.setQueryData([...PROPERTIES_KEY], [powerState]);
  qc.setQueryData([...COMMAND_TYPES_KEY], [power]);
  qc.setQueryData([...EVENT_RULES_KEY], [eventRule]);
  qc.setQueryData([...CALC_RULES_KEY], [calcRule]);
  qc.setQueryData([...ACTION_RULES_KEY], [actionRule]);
};

describe("catalog drawers link what they reference and list what uses them", () => {
  afterEach(() => { cleanup(); vi.restoreAllMocks(); });

  it("a product links its vendor, driver and type, lists its components, and shows what its driver collects", async () => {
    mount(Products, "/products?open=boreal-beam-340", (qc) => {
      seedCatalogs(qc);
      qc.setQueryData([...classifierPropertiesKey("product", "boreal-beam-340")], []);
      qc.setQueryData([...classifierMetricsKey("product", "boreal-beam-340")], []);
      qc.setQueryData([...componentUsageKey({ product: "boreal-beam-340" })], { rows: [comp("beam-1"), comp("beam-2")], total: 12 });
    });
    const b = await blade();
    expect(within(b).getByRole("link", { name: "Boreal" })).toHaveAttribute("href", "/vendors?open=boreal");
    expect(within(b).getByRole("link", { name: "Boreal Beam SNMP" })).toHaveAttribute("href", "/drivers?open=boreal-beam-snmp");
    expect(within(b).getByRole("link", { name: "Projector" })).toHaveAttribute("href", "/component-types?open=projector");
    // Collected by its driver: distinct from what the product declares.
    const collected = within(b).getByTestId("collected-by-driver");
    expect(within(collected).getByRole("link", { name: "uptime" })).toHaveAttribute("href", "/metrics?open=uptime");
    expect(within(collected).getByRole("link", { name: "power-state" })).toHaveAttribute("href", "/properties?open=power-state");
    expect(within(collected).getByRole("link", { name: "power-on" })).toHaveAttribute("href", "/command-types?open=power-on");
    // The component count is the server's whole count, the rows its first page.
    const comps = within(b).getByTestId("used-by-components");
    expect(within(comps).getByTestId("used-by-count")).toHaveTextContent("12");
    expect(within(comps).getByRole("link", { name: "beam-1" })).toHaveAttribute("href", `/components/${uuidFor("beam-1")}`);
    expect(within(comps).getByRole("link", { name: "See all 12" })).toBeInTheDocument();
  });

  it("a driver lists its products, its components, what it collects and the commands it offers", async () => {
    mount(Drivers, "/drivers?open=boreal-beam-snmp", (qc) => {
      seedCatalogs(qc);
      qc.setQueryData([...componentUsageKey({ driver: "boreal-beam-snmp" })], { rows: [comp("beam-1")], total: 1 });
    });
    const b = await blade();
    const products = within(b).getByTestId("used-by-products");
    expect(within(products).getByRole("link", { name: "Boreal Beam 340" })).toHaveAttribute("href", "/products?open=boreal-beam-340");
    expect(within(b).getByTestId("used-by-components")).toHaveTextContent("1");
    const collected = within(b).getByTestId("collected-by-driver");
    expect(within(collected).getByRole("link", { name: "uptime" })).toBeInTheDocument();
    expect(within(collected).getByRole("link", { name: "power-on" })).toHaveAttribute("href", "/command-types?open=power-on");
  });

  it("a vendor lists its products", async () => {
    mount(Vendors, "/vendors?open=boreal", seedCatalogs);
    const b = await blade();
    expect(within(within(b).getByTestId("used-by-products")).getByRole("link", { name: "Boreal Beam 340" })).toHaveAttribute("href", "/products?open=boreal-beam-340");
  });

  it("a component type links its parent and lists its products and components", async () => {
    mount(ComponentTypes, "/component-types?open=short-throw", (qc) => {
      seedCatalogs(qc);
      qc.setQueryData([...componentUsageKey({ component_type: "short-throw" })], { rows: [], total: 0 });
    });
    const b = await blade();
    expect(within(b).getByRole("link", { name: "Projector" })).toHaveAttribute("href", "/component-types?open=projector");
    expect(within(b).getByTestId("used-by-components")).toHaveTextContent("0");
  });

  it("a standard lists the systems built to it, linked to their pages", async () => {
    const std: Standard = { id: uuidFor("s"), name: "huddle", label: "Huddle", official: true };
    mount(Standards, "/standards?open=huddle", (qc) => {
      seedCatalogs(qc);
      qc.setQueryData([...STANDARDS_KEY], [std]);
      qc.setQueryData([...systemUsageKey("huddle")], { rows: [{ id: uuidFor("sys-1"), name: "huddle-1", label: "Huddle 1" }], total: 1 });
    });
    const b = await blade();
    expect(within(within(b).getByTestId("used-by-systems")).getByRole("link", { name: "Huddle 1" })).toHaveAttribute("href", `/systems/${uuidFor("sys-1")}`);
  });

  it("a metric names its writer class, and lists the products, drivers and rules that use it", async () => {
    mount(Metrics, "/metrics?open=uptime", (qc) => {
      seedCatalogs(qc);
      qc.setQueryData([...declaringKey("metric", "uptime")], [beam]);
    });
    const b = await blade();
    expect(within(b).getByTestId("written-by")).toHaveTextContent("Written by driver");
    expect(within(within(b).getByTestId("used-by-products-that-declare-it")).getByRole("link", { name: "Boreal Beam 340" })).toHaveAttribute("href", "/products?open=boreal-beam-340");
    expect(within(within(b).getByTestId("used-by-drivers-that-collect-it")).getByRole("link", { name: "Boreal Beam SNMP" })).toBeInTheDocument();
    const rules = within(b).getByTestId("used-by-rules-that-read-it");
    expect(within(rules).getByRole("link", { name: "AV offline" })).toHaveAttribute("href", "/event-rules?open=av-offline");
    expect(within(rules).getByRole("link", { name: "Uptime mean" })).toHaveAttribute("href", "/calc-rules?open=uptime-mean");
  });

  it("the metrics list names the writer class on every row no calc rule writes", async () => {
    mount(Metrics, "/metrics", seedCatalogs);
    await screen.findByText("TCP connect time");
    expect(screen.getAllByText("outpost probe").length).toBeGreaterThan(0);
    expect(screen.getAllByText("driver").length).toBeGreaterThan(0);
  });

  it("a property lists the drivers that collect it", async () => {
    mount(Properties, "/properties?open=power-state", (qc) => {
      seedCatalogs(qc);
      qc.setQueryData([...declaringKey("property", "power-state")], []);
    });
    const b = await blade();
    expect(within(within(b).getByTestId("used-by-drivers-that-collect-it")).getByRole("link", { name: "Boreal Beam SNMP" })).toBeInTheDocument();
    expect(within(b).getByTestId("used-by-products-that-declare-it")).toHaveTextContent("0");
  });

  it("a command type links its target and the drivers that offer it", async () => {
    mount(CommandTypes, "/command-types?open=power-on", seedCatalogs);
    const b = await blade();
    expect(within(b).getByRole("link", { name: "Power state" })).toHaveAttribute("href", "/properties?open=power-state");
    expect(within(within(b).getByTestId("used-by-drivers-that-offer-it")).getByRole("link", { name: "Boreal Beam SNMP" })).toBeInTheDocument();
  });

  it("a severity level lists the rules that raise at it", async () => {
    mount(SeverityLevels, "/severity-levels?open=high", (qc) => {
      seedCatalogs(qc);
      qc.setQueryData([...severityLevelsKey], [{ id: uuidFor("sev"), name: "high", label: "High", rank: 4, color: "error", outage: true, official: true }]);
    });
    const b = await blade();
    expect(within(within(b).getByTestId("used-by-rules")).getByRole("link", { name: "AV offline" })).toHaveAttribute("href", "/event-rules?open=av-offline");
  });

  it("a notification type lists the action rules that send it", async () => {
    mount(Notifications, "/notifications?open=alarm-page", (qc) => {
      seedCatalogs(qc);
      qc.setQueryData([...NOTIFICATION_TYPES_KEY], [{ id: uuidFor("nty"), name: "alarm-page", label: "Alarm page", subject: "s", text: "t", official: false }]);
    });
    const b = await blade();
    expect(within(within(b).getByTestId("used-by-action-rules")).getByRole("link", { name: "Page ops" })).toHaveAttribute("href", "/action-rules?open=page-ops");
  });

  it("an event rule links its severity, the key it reads and the events it writes", async () => {
    mount(Rules, "/event-rules?open=av-offline", (qc) => {
      seedCatalogs(qc);
      qc.setQueryData([...severityLevelsKey], [{ id: uuidFor("sev"), name: "high", label: "High", rank: 4, color: "error", outage: true, official: true }]);
    });
    const b = await blade();
    expect(hrefs(b)).toContain("/severity-levels?open=high");
    expect(hrefs(b)).toContain("/metrics?open=uptime");
    expect(hrefs(b)).toContain("/event-types?open=went-offline");
  });

  it("a calc rule links the keys it reads and the one it writes", async () => {
    mount(CalcRules, "/calc-rules?open=uptime-mean", seedCatalogs);
    const b = await blade();
    expect(hrefs(b)).toContain("/metrics?open=uptime");
    expect(hrefs(b)).toContain("/metrics?open=uptime-mean");
  });

  it("an action rule links its notification type and its targets", async () => {
    mount(ActionRules, "/action-rules?open=page-ops", (qc) => {
      seedCatalogs(qc);
      qc.setQueryData([...NOTIFICATION_TARGETS_KEY], [target]);
      qc.setQueryData([...NOTIFICATION_TYPES_KEY], [{ id: uuidFor("nty"), name: "alarm-page", label: "Alarm page", subject: "s", text: "t", official: false }]);
    });
    const b = await blade();
    expect(hrefs(b)).toContain("/notifications?open=alarm-page");
    expect(hrefs(b)).toContain("/notification-targets?open=ops-channel");
  });

  it("a notification target links the group or person it tells and the action rules that send to it", async () => {
    mount(NotificationTargets, "/notification-targets?open=ops-channel", (qc) => {
      seedCatalogs(qc);
      qc.setQueryData([...NOTIFICATION_TARGETS_KEY], [{ ...target, targets: undefined, ...{} }]);
      qc.setQueryData([...ACTION_RULES_KEY], [{ ...actionRule, targets: ["ops-channel"] }]);
    });
    const b = await blade();
    expect(hrefs(b)).toContain("/user-groups?open=help-desk");
    expect(hrefs(b)).toContain("/action-rules?open=page-ops");
  });
});
