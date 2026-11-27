import { describe, it, expect, afterEach, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider, focusManager } from "@tanstack/solid-query";
import Components from "./Components";
import { withTopBar } from "../lib/testheader";
import { FLEET_VIEW_KEY, type FleetView } from "../lib/fleet";
import { COMPONENTS_KEY, type Component } from "../lib/components";
import { componentSystemsKey } from "../lib/members";
import { REACHABILITY_KEY, type Reachability } from "../lib/reachability";
import { componentAlarmsReadKey } from "../lib/alarms";
import { effectivePropertiesKey } from "../lib/component_properties";
import { effectiveMetricsKey } from "../lib/component_metrics";
import { metricBucketsKey } from "../lib/series";
import { outpostKey, type Outpost } from "../lib/outposts";
import { PRODUCTS_KEY } from "../lib/products";
import { LOCATIONS_KEY } from "../lib/locations";
import { LOCATION_TYPES_KEY } from "../lib/location_types";
import { TAGS_KEY } from "../lib/tags";
import { systemSlotsKey } from "../lib/system_slots";
import { ME_KEY, type Me } from "../lib/auth";
import { componentCommandsKey, type Command } from "../lib/commands";
import { EVENTS_KEY } from "../lib/events";
import { ENDPOINTS_KEY } from "../lib/endpoints";
import { DRIVERS_KEY } from "../lib/drivers";
import { uuidFor } from "../lib/testids";
import { LOGS_KEY } from "../lib/logs";
import { seedSeverityLevels } from "../lib/testseverity";
import { tipOf } from "../test-tip";
import { pageHeader } from "../lib/handover";

const acked: string[] = [];
vi.mock("../lib/alarms", async (orig) => {
  const real = await orig<typeof import("../lib/alarms")>();
  return { ...real, acknowledgeAlarm: vi.fn(async (name: string, id: string) => { acked.push(`${name}:${id}`); return {}; }) };
});

// The component leaf (#637): direct arrival renders the breadcrumb from the
// ancestor chain, memberships list with the primary marked, and the
// collection card states the distinction plainly: a stale sample under a
// healthy outpost blames the component or the path, never collection.

const me: Me = { user: { id: "u-root", kind: "person" }, person: { username: "root" }, permissions: [">"], grants: [] };

const view: FleetView = {
  locations: [
    { id: uuidFor("cf-hq"), name: "hq", label: "Headquarters", location_type: "campus", location_type_id: uuidFor("cft-campus"), parent: "", health: "healthy" },
    { id: uuidFor("cf-room"), name: "boardroom-a", label: "Boardroom A", location_type: "room", location_type_id: uuidFor("cft-room"), parent: uuidFor("cf-hq"), health: "healthy" },
  ],
  systems: [
    { id: uuidFor("cf-s-a"), name: "boardroom", label: "Boardroom System", location: uuidFor("cf-room"), health: "degraded", dots: [{ component: uuidFor("cf-c-bar"), name: "videobar-1", health: "outage", primary: true, shared: false }] },
    { id: uuidFor("cf-s-b"), name: "overflow", label: "Overflow", location: null, health: "healthy", dots: [] },
  ],
} as unknown as FleetView;

const bar: Component = {
  id: uuidFor("cf-c-bar"),
  name: "videobar-1",
  label: "Video Bar 1",
  location: "boardroom-a",
  location_id: uuidFor("cf-room"),
  product: "kestrel-vroom",
} as unknown as Component;

const now = Date.now();
const iso = (secondsAgo: number) => new Date(now - secondsAgo * 1000).toISOString();

const reach: Reachability = {
  component: "videobar-1",
  endpoints: [
    // A stale sample: last health long ago, but the outpost below is alive.
    { endpoint: "ssh", transport: "ssh", outpost: "edge-1", reachability: { value: "up", ts: iso(600) }, layers: [
      { layer: "ping", check: "icmp-reachable", value: "up", ts: iso(600) },
      { layer: "port", check: "tcp-open", value: "open", ts: iso(600) },
    ], history: [] },
  ],
} as unknown as Reachability;

const outposts: Outpost[] = [{ name: "edge-1", enrolled: true, last_heartbeat_at: iso(5), tags: {} } as Outpost];

// The cache the last mount rendered from, so a test can land a poll on it.
let leafClient: QueryClient | undefined;

function mount(path = `/web/components/${uuidFor("cf-c-bar")}`, memberships?: unknown[], component: Component = bar, meOverride: Me = me, alarms?: unknown[], seed?: { reach?: Reachability; outposts?: Outpost[]; commands?: Command[]; metrics?: unknown[]; view?: FleetView; alarmsHealth?: string }, components?: Component[]) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  leafClient = qc;
  seedSeverityLevels(qc);
  qc.setQueryData([...FLEET_VIEW_KEY], seed?.view ?? view);
  qc.setQueryData([...COMPONENTS_KEY], components ?? [component]);
  qc.setQueryData([...PRODUCTS_KEY], [{ id: uuidFor("cf-p"), name: "kestrel-vroom", label: "Kestrel VRoom", vendor: "kestrel", driver: "kestrel-http" }]);
  for (const n of seed?.outposts ?? outposts) qc.setQueryData([...outpostKey(n.name)], n);
  qc.setQueryData([...LOCATIONS_KEY], []);
  qc.setQueryData([...LOCATION_TYPES_KEY], [{ id: uuidFor("cft-room"), name: "room", label: "Room", allowed_parent_types: [] }]);
  qc.setQueryData([...systemSlotsKey(uuidFor("cf-s-a"))], [{ name: "conf-bar", label: "Conferencing Bar", assigned_to: ["videobar-1"] }]);
  qc.setQueryData([...systemSlotsKey(uuidFor("cf-s-b"))], []);
  qc.setQueryData([...TAGS_KEY], []);
  qc.setQueryData([...ME_KEY], meOverride);
  qc.setQueryData([...componentSystemsKey(uuidFor("cf-c-bar"))], memberships ?? [
    { component: "videobar-1", system: "boardroom", primary: true, system_count: 2 },
    { component: "videobar-1", system: "overflow", primary: false, system_count: 2 },
  ]);
  qc.setQueryData([...REACHABILITY_KEY(uuidFor("cf-c-bar"))], seed?.reach ?? reach);
  qc.setQueryData([...componentAlarmsReadKey(uuidFor("cf-c-bar"))], { ...(seed?.alarmsHealth ? { health: seed.alarmsHealth } : {}), alarms: alarms ?? [
    { id: "al-1", component: "videobar-1", component_id: uuidFor("cf-c-bar"), severity: "high", message: "No route to host", raised_at: iso(9600), active: true, acknowledged: false },
    { id: "al-0", component: "videobar-1", component_id: uuidFor("cf-c-bar"), severity: "warning", message: "Cleared last week", raised_at: iso(600000), active: false, acknowledged: true },
  ] });
  qc.setQueryData([...effectivePropertiesKey(uuidFor("cf-c-bar"))], [
    { property_type_name: "serial-number", label: "Serial Number", value: "SN-1183", is_set: true, from_contract: true },
    { property_type_name: "firmware-version", value: "2.1.4", is_set: false, from_contract: true },
    { property_type_name: "mac-address", value: null, is_set: false, from_contract: true },
  ]);
  qc.setQueryData([...effectiveMetricsKey(uuidFor("cf-c-bar"))], seed?.metrics ?? [
    { metric_type_name: "icmp-rtt-avg", label: "RTT avg", data_type: "float", value: 6.1, is_sampled: true, from_contract: false, required: false },
  ]);
  qc.setQueryData([...componentCommandsKey(uuidFor("cf-c-bar"))], seed?.commands ?? []);
  qc.setQueryData([...EVENTS_KEY(uuidFor("cf-c-bar"))], { component: "videobar-1", events: [] });
  qc.setQueryData([...ENDPOINTS_KEY], []);
  qc.setQueryData([...DRIVERS_KEY], []);
  window.history.pushState({}, "", path);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router base="/web">
        <Route path="/components/:id" component={withTopBar(Components)} />
        <Route path="/explore" component={() => <div data-testid="fleet-page" />} />
      </Router>
    </QueryClientProvider>
  ));
}

afterEach(cleanup);

describe("the component leaf", () => {
  // The Timeline view (#2425): the overlay and the metrics grid on one cursor,
  // under the page's one time range.
  it("offers a Timeline view with the time range, the overlay and the metrics on it", () => {
    mount(`/web/components/${uuidFor("cf-c-bar")}?tab=timeline`);
    expect(screen.getByRole("tab", { name: "Timeline" })).toBeTruthy();
    expect(screen.getByTestId("timeline-overlay")).toBeTruthy();
    expect(screen.getByTestId("time-range-trigger")).toBeTruthy();
  });

  it("links Explore back to this component's row in the outline, first crumb of line 2", async () => {
    mount();
    const trail = screen.getByTestId("breadcrumb");
    expect(within(trail).getByRole("link", { name: "Explore" })).toBeTruthy();
    expect(within(trail).getAllByRole("link").filter((a) => !a.closest('[data-testid="lead-system"]'))[0].textContent).toBe("Explore");
    fireEvent.click(within(trail).getByRole("link", { name: "Explore" }));
    expect(await screen.findByTestId("fleet-page")).toBeTruthy();
    expect(window.location.pathname + window.location.search).toBe(`/web/explore?node=${uuidFor("cf-c-bar")}`);
  });

  // The two-line header (#2575): line 1 is the kind's list, the label as the one
  // h1 and the primary system; line 2 is the location path, as links.
  it("line 1 reads Components / label (system), line 2 the location path, one h1, no body breadcrumb", () => {
    mount();
    const bar = document.querySelector("header")!;
    const line1 = within(bar).getByTestId("header-line-1");
    expect(within(line1).getByRole("link", { name: "Components" })).toBeTruthy();
    expect(within(line1).getByRole("link", { name: "Boardroom System" })).toBeTruthy();
    expect(line1.textContent).toBe("Components/Video Bar 1(Boardroom System)");
    const trail = within(bar).getByTestId("breadcrumb");
    expect(line1.contains(trail)).toBe(false);
    expect(within(trail).getByRole("link", { name: "Headquarters" })).toBeTruthy();
    expect(within(trail).queryByText("Video Bar 1")).toBeNull();
    const h1s = screen.getAllByRole("heading", { level: 1 });
    expect(h1s).toHaveLength(1);
    expect(h1s[0].textContent).toBe("Video Bar 1");
    expect(pageHeader()?.title).toBe("Video Bar 1");
    expect(screen.getAllByTestId("breadcrumb")).toHaveLength(1);
    expect(document.querySelector("section")!.querySelector('[data-testid="breadcrumb"]')).toBeNull();
  });

  it("says what it is: product (label with its handle), vendor, driver, once each", () => {
    mount();
    const card = screen.getByTestId("leaf-identity");
    expect(within(card).getByText("kestrel-vroom")).toBeTruthy();
    expect(within(card).getByText("kestrel")).toBeTruthy();
    expect(within(card).getByText("kestrel-http")).toBeTruthy();
    expect(within(card).queryByText(/driven by/)).toBeNull();
  });

  // Each fact once (#872 audit): in the room its one system makes, the location
  // and the system are one thing, already named by "Systems it serves" (and
  // the path), so no location card says it again.
  it("shows no location card where its location is the room of a system it serves", () => {
    mount();
    expect(screen.queryByTestId("location-card")).toBeNull();
    expect(within(screen.getByTestId("leaf-memberships")).getByText("Boardroom System")).toBeTruthy();
  });

  // A system is its name and its address (#2757): "Boardroom System" at the
  // room "Boardroom A" reads as the two, the address beside the name.
  it("lists each system it serves by name with its address beside it", () => {
    mount();
    const row = within(screen.getByTestId("leaf-memberships")).getByText("Boardroom System", { selector: "[data-system-label]" }).closest("[data-testid=system-name]") as HTMLElement;
    expect(within(row).getByText("Boardroom A", { selector: "[data-system-address]" })).toBeTruthy();
    // An unplaced system has no address to add.
    const overflow = within(screen.getByTestId("leaf-memberships")).getByText("Overflow", { selector: "[data-system-label]" }).closest("[data-testid=system-name]") as HTMLElement;
    expect(overflow.querySelector("[data-system-address]")).toBeNull();
  });

  it("shows a location card, marked as its own, where it sits apart from the systems it serves", () => {
    mount(undefined, undefined, { ...bar, location: "hq", location_id: uuidFor("cf-hq") } as unknown as Component);
    const card = screen.getByTestId("location-card");
    expect(within(card).getByText("Headquarters")).toBeTruthy();
    expect(within(card).getByTestId("location-provenance").textContent).toBe("set here");
  });

  it("names its product once, under what it is, not in the header too", () => {
    mount();
    expect(within(screen.getByTestId("leaf-header")).queryByText("Kestrel VRoom")).toBeNull();
    expect(within(screen.getByTestId("leaf-identity")).getByText("Kestrel VRoom")).toBeTruthy();
  });

  it("carries no counts line counting itself (#872)", () => {
    mount();
    expect(screen.queryByTestId("counts-line")).toBeNull();
    expect(screen.queryByTestId("fleet-summary")).toBeNull();
  });

  it("names the slot it fills in each system it serves, the primary marked, in no inline prose", async () => {
    mount();
    const card = screen.getByTestId("leaf-memberships");
    expect(within(card).getByText("Boardroom System")).toBeTruthy();
    expect(within(card).getByText("Overflow")).toBeTruthy();
    expect(await within(card).findByText("Conferencing Bar")).toBeTruthy();
    expect(within(card).getByText("primary")).toBeTruthy();
    expect(within(card).queryByText(/Location follows/)).toBeNull();
  });

  it("distinguishes a stale sample under a healthy outpost from an offline outpost", () => {
    mount();
    const chip = screen.getByTestId("collection-ssh");
    expect(chip.textContent).toContain("check the component or the path");
    // A component that answers badly is a component fact, in the product's
    // own word for it.
    expect(screen.getByTestId("leaf-collection").textContent).not.toMatch(/device/);
  });

  it("a component filling no slot renders without error", () => {
    mount(`/web/components/${uuidFor("cf-c-bar")}`, []);
    expect(screen.getByText(/Not in any system yet/)).toBeTruthy();
    expect(screen.getByTestId("leaf-collection")).toBeTruthy();
  });

  it("a name-shaped address with two components named mic-1 shows ambiguous message instead of redirecting", () => {
    const mic1_a: Component = {
      id: uuidFor("cf-c-mic1-a"),
      name: "mic-1",
      label: "Mic 1 A",
      location: "boardroom-a",
      location_id: uuidFor("cf-room"),
      product: "kestrel-vroom",
    } as unknown as Component;
    const mic1_b: Component = {
      id: uuidFor("cf-c-mic1-b"),
      name: "mic-1",
      label: "Mic 1 B",
      location: "boardroom-a",
      location_id: uuidFor("cf-room"),
      product: "kestrel-vroom",
    } as unknown as Component;
    mount(`/web/components/mic-1`, undefined, mic1_a, me, undefined, undefined, [mic1_a, mic1_b]);
    expect(screen.getByText(/More than one component/i)).toBeTruthy();
  });

  it("a name-shaped address with one component named mic-1 redirects to its id", async () => {
    const mic1: Component = {
      id: uuidFor("cf-c-mic1"),
      name: "mic-1",
      label: "Mic 1",
      location: "boardroom-a",
      location_id: uuidFor("cf-room"),
      product: "kestrel-vroom",
    } as unknown as Component;
    mount(`/web/components/mic-1`, undefined, mic1, me, undefined, undefined, [mic1]);
    await waitFor(() => {
      expect(window.location.pathname).toBe(`/web/components/${uuidFor("cf-c-mic1")}`);
    });
  });
});

// A device nothing has ever collected from reads the same on every surface
// (#1454): an unknown health, no outpost offline when no outpost exists, and no
// ladder chip standing in for a probe that never ran. The outline reads the
// same device as a health-less row, which the badge words as unknown.
describe("a device nothing has collected from (#1454)", () => {
  const looseView = { ...view, systems: [] } as unknown as FleetView;
  const neverProbed = {
    component: "videobar-1",
    endpoints: [{ endpoint: "icmp", transport: "icmp", reachability: null, layers: [], history: [] }],
  } as unknown as Reachability;
  const mountNever = () => mount(undefined, [], bar, me, [], { reach: neverProbed, outposts: [], view: looseView, alarmsHealth: "unknown" });

  it("wears an unknown badge, not healthy, as the outline does", () => {
    mountNever();
    const header = screen.getByTestId("leaf-header");
    expect(within(header).getByText("unknown")).toBeTruthy();
    expect(within(header).queryByText("healthy")).toBeNull();
  });

  it("says no outpost collects it, never that an outpost is offline", () => {
    mountNever();
    const chip = screen.getByTestId("collection-icmp");
    expect(chip.textContent).toBe("no outpost collecting it");
    expect(screen.queryByText("outpost offline")).toBeNull();
  });

  it("shows no reachability rung without a stored health", () => {
    mountNever();
    const card = screen.getByTestId("leaf-collection");
    expect(within(card).queryByText("reachable")).toBeNull();
    expect(within(card).queryAllByTestId("ladder-rung")).toHaveLength(0);
  });

  it("shows the rung once a probe has answered", () => {
    mount(undefined, [], bar, me, [], {
      reach: { ...neverProbed, endpoints: [{ endpoint: "icmp", transport: "icmp", reachability: { value: "up", ts: iso(5) }, last_seen: iso(5), layers: [{ layer: "ping", check: "icmp-reachable", value: "up", ts: iso(5) }], history: [] }] } as unknown as Reachability,
      outposts: [], view: looseView, alarmsHealth: "healthy",
    });
    expect(within(screen.getByTestId("leaf-collection")).getByText("reachable")).toBeTruthy();
  });
});

describe("the leaf's dispatch facts (#786)", () => {
  it("the header wears the component's health and since-when from the worst active alarm", () => {
    mount();
    const header = screen.getByTestId("leaf-header");
    expect(within(header).getByText("outage")).toBeTruthy();
    expect(within(header).getByText(/since/)).toBeTruthy();
  });

  it("renders the active alarms with severity, message and ack state, and never the cleared history", () => {
    mount();
    const card = screen.getByTestId("leaf-alarms");
    expect(within(card).getByText("No route to host")).toBeTruthy();
    expect(within(card).getByText(/unacknowledged/)).toBeTruthy();
    expect(within(card).queryByText("Cleared last week")).toBeNull();
    // Each alarm row opens its timeline in place (#950).
    expect(within(card).getAllByRole("button", { name: /^Timeline of / }).length).toBeGreaterThan(0);
  });

  // The strip's tone follows the catalog's outage flag (#917), not a level name:
  // high and disaster take the component out and read red, average only degrades.
  it.each([
    ["high", "High", true],
    ["disaster", "Disaster", true],
    ["average", "Average", false],
  ])("tones the Why strip by the outage flag of %s", (severity, label, outage) => {
    mount(undefined, undefined, undefined, undefined, [
      { id: "al-x", component: "videobar-1", component_id: uuidFor("cf-c-bar"), severity, message: "Something", raised_at: iso(60), active: true, acknowledged: false },
    ]);
    const card = screen.getByTestId("leaf-alarms");
    expect(card.classList.contains("border-error/40")).toBe(outage);
    expect(card.classList.contains("border-warning/40")).toBe(!outage);
    expect(within(card).getByText(label)).toBeTruthy();
  });

  it("the identity panel carries the resolved properties: serial and firmware beside product, vendor, driver", () => {
    mount();
    const card = screen.getByTestId("leaf-identity");
    expect(within(card).getByText("SN-1183")).toBeTruthy();
    expect(within(card).getByText("2.1.4")).toBeTruthy();
    // A property with no value is not a row.
    expect(within(card).queryByText(/mac-address/)).toBeNull();
  });

  it("vitals list the effective metrics that carry a value, marking the live series", () => {
    mount();
    const card = screen.getByTestId("leaf-vitals");
    expect(within(card).getByText("RTT avg")).toBeTruthy();
    expect(within(card).getByText("6.1")).toBeTruthy();
  });

  // #2424: the component page charts what Vitals lists, over the page's
  // range (the address's ?range), one card per effective metric.
  it("charts every effective metric in the metrics grid over the page's range", async () => {
    mount(`/web/components/${uuidFor("cf-c-bar")}?range=1h`);
    const t = new Date(Date.now() - 10 * 60_000).toISOString();
    leafClient!.setQueryData([...metricBucketsKey("components", uuidFor("cf-c-bar"), "icmp-rtt-avg", 1, 60)], {
      coveredFrom: null,
      buckets: [{ ts: t, count: 2, min: 5.9, max: 6.3, avg: 6.1, last: 6.1 }],
    });
    const grid = await screen.findByTestId("metrics-grid");
    expect(within(grid).getByText("Last 1 hour")).toBeTruthy();
    const card = within(grid).getByTestId("metric-card-icmp-rtt-avg");
    expect(await within(card).findByTestId("timeseries-chart")).toBeTruthy();
  });

  it("the range picker on the tab rail re-reads the metrics grid over the picked range", async () => {
    mount();
    expect(await screen.findByTestId("metrics-grid")).toBeTruthy();
    fireEvent.click(screen.getByTestId("time-range-trigger"));
    fireEvent.click(await screen.findByTestId("time-range-preset-7d"));
    await waitFor(() => expect(window.location.search).toContain("range=7d"));
    await waitFor(() => expect(within(screen.getByTestId("metrics-grid")).getByText("Last 7 days")).toBeTruthy());
    await waitFor(() => expect(leafClient!.getQueryCache().find({ queryKey: [...metricBucketsKey("components", uuidFor("cf-c-bar"), "icmp-rtt-avg", 168, 3600)] })).toBeTruthy());
  });

  // #2432: a vitals row offers "Alarm on this", which opens the new-rule form
  // reading that metric on this component.
  it("a vitals row links to a new rule reading that metric on this component", () => {
    const admin: Me = { ...me, fleet_permissions: [">"], grants: [{ role: "owner", scope_kind: "all" }] };
    mount(undefined, undefined, undefined, admin, undefined, {
      metrics: [{ metric_type_name: "uptime", label: "Uptime", data_type: "float", unit: "s", value: 306, is_sampled: true, from_contract: false, required: false }],
    });
    const link = within(screen.getByTestId("vital-uptime")).getByRole("link", { name: "Alarm on Uptime" });
    expect(link.getAttribute("href")).toBe(`/web/event-rules?new=1&reads=uptime&component=${uuidFor("cf-c-bar")}`);
  });

  // #1550: a metric's unit rides after its value in the muted style, and a
  // float is rounded to a sane precision with the raw value in the title.
  it("a vitals value carries its unit, rounded, with the raw value in the title", async () => {
    mount(undefined, undefined, undefined, undefined, undefined, {
      metrics: [
        { metric_type_name: "uptime", label: "Uptime", data_type: "float", unit: "s", value: 306, is_sampled: true, from_contract: false, required: false },
        { metric_type_name: "poll-duration", label: "Poll Duration", data_type: "float", unit: "ms", value: 0.1527895, is_sampled: true, from_contract: false, required: false },
        { metric_type_name: "icmp-rtt-avg", label: "RTT avg", data_type: "float", value: 6.1, is_sampled: true, from_contract: false, required: false },
      ],
    });
    expect(screen.getByTestId("vital-uptime").textContent).toContain("306 s");
    const poll = screen.getByTestId("vital-poll-duration");
    expect(poll.textContent).toContain("0.15 ms");
    expect(poll.textContent).not.toContain("0.1527895");
    expect(await tipOf(poll.querySelector("[tabindex]"))).toBe("0.1527895");
    // A type with no unit reads as a bare number, as before.
    expect(screen.getByTestId("vital-icmp-rtt-avg").textContent).toContain("6.1");
    expect(screen.getByTestId("vital-icmp-rtt-avg").querySelector('[data-testid="vital-unit"]')).toBeNull();
  });

  // #1379: a vitals row reads as one line. The label is the card's text and
  // the value smaller monospace; a grid row stretches its cells and starts
  // each at the top, which lifts the value above its label, so the grid
  // aligns every row on its first baseline instead.
  it("a vitals value shares its label's baseline", () => {
    mount();
    const list = screen.getByTestId("leaf-vitals").querySelector("dl");
    expect(list?.classList.contains("items-baseline")).toBe(true);
  });

  // The same rows in the identity card above it: a monospace driver or
  // model number beside the card's text label (#1379's review).
  it("an identity value shares its label's baseline", () => {
    mount();
    const list = screen.getByTestId("leaf-identity").querySelector("dl");
    expect(list?.classList.contains("items-baseline")).toBe(true);
  });

  // #1020: a metric two endpoints collect shows the newest sample, and its
  // row says which endpoint that is and of how many, with a tooltip saying
  // why, and opens to each endpoint's latest. A single-instance metric reads
  // as it always did.
  const spreadMetrics = (connect: number) => [
    {
      metric_type_name: "tcp-connect-time", label: "TCP Connect Time", data_type: "float", value: connect, is_sampled: true, from_contract: false, required: false,
      instance: "tcp", instance_count: 2,
      instances: [{ instance: "tcp", value: connect, ts: iso(30), endpoint: true }, { instance: "http", value: 2.2, ts: iso(45), endpoint: true }],
    },
    { metric_type_name: "icmp-rtt-avg", label: "ICMP RTT (avg)", data_type: "float", value: 1, is_sampled: true, from_contract: false, required: false, instance: "icmp", instance_count: 1 },
  ];

  it("a metric two endpoints collect names the endpoint it shows and the count, and opens to each", () => {
    mount(undefined, undefined, bar, me, undefined, { metrics: spreadMetrics(4.8) });
    const card = screen.getByTestId("leaf-vitals");
    const toggle = within(card).getByRole("button", { name: /latest of 2 endpoints \(tcp\)/ });
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    // The single-instance metric carries no caption.
    expect(within(screen.getByTestId("vital-spread-icmp-rtt-avg")).queryByText(/latest of/)).toBeNull();
    expect(within(card).getAllByText(/latest of/)).toHaveLength(1);
    expect(screen.queryByTestId("vital-instances-tcp-connect-time")).toBeNull();

    fireEvent.click(toggle);
    expect(toggle.getAttribute("aria-expanded")).toBe("true");
    const list = screen.getByTestId("vital-instances-tcp-connect-time");
    expect(toggle.getAttribute("aria-controls")).toBe(list.id);
    const lines = within(list).getAllByRole("listitem").map((li) => li.textContent ?? "");
    expect(lines).toHaveLength(2);
    expect(lines[0]).toContain("tcp");
    expect(lines[0]).toContain("4.8");
    expect(lines[1]).toContain("http");
    expect(lines[1]).toContain("2.2");
  });

  it("the open row survives a poll that brings new values", async () => {
    mount(undefined, undefined, bar, me, undefined, { metrics: spreadMetrics(4.8) });
    fireEvent.click(within(screen.getByTestId("leaf-vitals")).getByRole("button", { name: /latest of 2 endpoints/ }));
    leafClient!.setQueryData([...effectiveMetricsKey(uuidFor("cf-c-bar"))], spreadMetrics(5.1));
    await waitFor(() => expect(screen.getByTestId("vital-tcp-connect-time").textContent).toContain("5.1"));
    const list = screen.getByTestId("vital-instances-tcp-connect-time");
    expect(list.textContent).toContain("5.1");
  });

  it("the caption's tooltip says why the row shows one endpoint's value", async () => {
    mount(undefined, undefined, bar, me, undefined, { metrics: spreadMetrics(4.8) });
    const tip = within(screen.getByTestId("vital-spread-tcp-connect-time")).getByRole("button", { name: /More about/ });
    fireEvent.keyDown(document.body, { key: "Tab" });
    tip.focus();
    expect(await screen.findByText(/newest sample across them/)).toBeTruthy();
  });

  it("the collection card shows each endpoint's ladder, bottom up", () => {
    mount();
    const card = screen.getByTestId("leaf-collection");
    const rungs = within(card).getAllByTestId("ladder-rung").map((r) => `${r.dataset.rung}:${r.dataset.status}`);
    expect(rungs).toEqual(["reachable:pass", "responsive:pass"]);
  });

  // A held session on the component's view (#1059): the pill, and a rejected
  // session's reason with Retry, where a viewer reads Retry greyed with why.
  const rejectedReach = {
    component: "videobar-1",
    endpoints: [{
      id: uuidFor("cf-ep-telnet"), endpoint: "telnet", transport: "telnet", outpost: "edge-1", last_seen: iso(2),
      reachability: { value: "up", ts: iso(60) }, responsive: { value: "up", ts: iso(60) }, authenticated: { value: "no", ts: iso(30) },
      session: { state: "rejected", ts: iso(30), reason: "auth: auth rejected" }, rejection: { rung: "authenticated", class: "auth", ts: "2026-10-23T11:58:00.000Z" },
      layers: [{ layer: "ping", check: "icmp-reachable", value: "up", ts: iso(2) }], history: [],
    }],
  } as unknown as Reachability;

  it("the collection card reads a rejected session, its reason, and Retry", () => {
    mount(undefined, undefined, bar, me, undefined, { reach: rejectedReach });
    const card = screen.getByTestId("leaf-collection");
    expect(within(card).getByTestId("session-pill").dataset.state).toBe("rejected");
    expect(within(card).getByTestId("session-reason").textContent).toBe("auth: auth rejected");
    expect(within(card).getByTestId("collection-telnet").textContent).toBe("polls stopped");
    expect((within(card).getByRole("button", { name: /retry/i }) as HTMLButtonElement).disabled).toBe(false);
  });

  // #1110: an endpoint gated by its reachability probe alone holds no
  // session, and its refusal reads on the card all the same.
  const probeGatedReach = {
    component: "videobar-1",
    endpoints: [{
      id: uuidFor("cf-ep-snmp"), endpoint: "snmp", transport: "snmp", outpost: "edge-1", last_seen: iso(2),
      reachability: { value: "down", ts: iso(60) }, responsive: { value: "down", ts: iso(60) }, authenticated: { value: "no", ts: iso(60) },
      rejection: { rung: "authenticated", class: "auth", ts: iso(60), reason: "auth: the agent answered authorizationError for the community" },
      layers: [{ layer: "ping", check: "icmp-reachable", value: "up", ts: iso(2) }], history: [],
    }],
  } as unknown as Reachability;

  it("the collection card reads a probe's refusal with no held session, its reason, and Retry", () => {
    mount(undefined, undefined, bar, me, undefined, { reach: probeGatedReach });
    const card = screen.getByTestId("leaf-collection");
    expect(within(card).queryByTestId("session-pill")).toBeNull();
    expect(within(card).getByTestId("session-reason").textContent).toBe("auth: the agent answered authorizationError for the community");
    // Beneath the community's gate no get is sent, the poll's included.
    expect(within(card).getByTestId("collection-snmp").textContent).toBe("polls stopped");
    expect((within(card).getByRole("button", { name: /retry/i }) as HTMLButtonElement).disabled).toBe(false);
  });

  // The view polls the read and every poll hands back fresh objects (last
  // seen moves): rows are kept by endpoint id, so a rung's open tooltip is
  // not torn out from under the pointer by the next poll.
  it("keeps a collection row's elements across a poll that hands back fresh objects", async () => {
    const { container } = mount(undefined, undefined, bar, me, undefined, { reach: rejectedReach });
    const card = screen.getByTestId("leaf-collection");
    const rung = within(card).getAllByTestId("ladder-rung")[0];
    const qcReach = { ...rejectedReach, endpoints: rejectedReach.endpoints.map((e) => ({ ...e, last_seen: new Date(Date.now() + 5_000).toISOString() })) };
    // Re-seed through the same cache the view reads, as its poll would.
    leafClient!.setQueryData([...REACHABILITY_KEY(uuidFor("cf-c-bar"))], qcReach);
    await new Promise((r) => setTimeout(r, 20));
    expect(container.contains(rung)).toBe(true);
  });

  it("a viewer reads the rejected session with Retry greyed and its reason", () => {
    const viewerMe = { user: { id: "vw", kind: "person" }, permissions: ["*:read"], grants: [] } as unknown as Me;
    mount(undefined, undefined, bar, viewerMe, undefined, { reach: rejectedReach });
    const card = screen.getByTestId("leaf-collection");
    expect((within(card).getByRole("button", { name: /retry/i }) as HTMLButtonElement).disabled).toBe(true);
    expect(within(card).getByLabelText(/needs the endpoint:retry permission/i)).toBeTruthy();
  });
});


// The leaf grows the Configure facet too (#800 slice 1): identity with the
// reset-to-generated affordance, the product read-only (fixed at creation),
// tags. Placement reads where it sits; movers are not part of the classic
// contract this slice reproduces.
describe("the component configure tab (#800)", () => {
  it("offers Configure with identity, the fixed product, and tags", async () => {
    mount(`/web/components/${uuidFor("cf-c-bar")}?tab=configure`);
    const face = await screen.findByTestId("configure-face");
    expect(within(face).getByText("Identity")).toBeTruthy();
    expect(within(face).getByText("Classification")).toBeTruthy();
    expect(within(face).getAllByText("Tags").length).toBeGreaterThan(0);
    // The product reads as its label (#870); fixed-at-creation is its tooltip.
    expect(within(face).getByText("Kestrel VRoom")).toBeTruthy();
  });
});


// The interfaces surface the classic face carried, re-homed on the leaf's
// Configure (#800 slice 3): the reachability panel with its add and open
// affordances wiring the interface blades onto the leaf's own stack.
describe("endpoints live on the leaf configure (#800)", () => {
  it("offers Add endpoint and opens the create blade on this stack", async () => {
    mount(`/web/components/${uuidFor("cf-c-bar")}?tab=configure`);
    const face = await screen.findByTestId("configure-face");
    const add = await within(face).findByRole("button", { name: /add endpoint/i });
    fireEvent.click(add);
    const blade = await screen.findByRole("dialog");
    expect(blade.getAttribute("aria-labelledby")).toContain("endpoint-create");
  });
});

describe("the miss face (#800)", () => {
  // What it was told (#1155): the Activity tab carries the commands above the
  // events, for a caller who may read them, and opens the one an issue's
  // toast asks for.
  it("carries its commands on Activity, opening the one asked for", async () => {
    const cmd = {
      id: 12, component: "videobar-1", component_id: uuidFor("cf-c-bar"), command_type: "set-volume", intended: 42,
      target_kind: "metric", target: "volume", status: "settled", phase: "settled", settle_mode: "reply", settled_by: "reply",
      reason: "the reply carried 42, the intended value", reply: '{"type":"result","value":42}', settle_window_seconds: 15,
      deadline: iso(0), issued_at: iso(15), settled_at: iso(14), issuer: "root", issuer_id: "u-root",
    } as unknown as Command;
    mount(`/web/components/${uuidFor("cf-c-bar")}?tab=activity&command=12`, undefined, bar, me, undefined, { commands: [cmd] });
    const panel = await screen.findByTestId("commands-panel");
    await waitFor(() => expect(within(panel).getByTestId("command-reply").textContent).toBe('{"type":"result","value":42}'));
    expect(within(screen.getByTestId("activity-tab")).getByText("Events")).toBeTruthy();
  });

  it("carries no commands for a caller who may not read them", () => {
    mount(`/web/components/${uuidFor("cf-c-bar")}?tab=activity`, undefined, bar, { ...me, permissions: ["component:read", "event:read", "location:read", "system:read", "product:read"] });
    expect(screen.queryByTestId("commands-panel")).toBeNull();
  });

  it("an address matching no component renders the explicit miss, not a silent fallback", async () => {
    mount("/web/components/no-such-widget");
    expect(await screen.findByText(/No component answers this address/)).toBeTruthy();
    expect(screen.queryByText("Reachability")).toBeNull();
  });
});

// Acknowledging is outside the update gate (ADR-0109): it records that the
// reader looked and writes none of the component's data. With the alarms
// panel on Configure only, a caller holding alarm:acknowledge without
// component:update had no way left to acknowledge (#872 review). Why carries
// it, on its own permission.
describe("acknowledging from Why (#872)", () => {
  const ackOnly: Me = { user: { id: "u-ack", kind: "person" }, person: { username: "ack" }, permissions: ["component:read", "system:read", "location:read", "alarm:read", "alarm:acknowledge"], grants: [] };

  it("offers Acknowledge on an unacknowledged alarm to a caller holding only the acknowledge permission", async () => {
    acked.length = 0;
    mount(undefined, undefined, bar, ackOnly);
    const why = screen.getByTestId("leaf-alarms");
    fireEvent.click(within(why).getByRole("button", { name: "Acknowledge" }));
    await waitFor(() => expect(acked).toEqual([`${uuidFor("cf-c-bar")}:al-1`]));
  });

  // A zero-basis message never wraps onto its own line, so a long word
  // overflowed into the "by rule" label beside it. It needs a real basis.
  it("gives the Why message a flex basis so it wraps instead of overflowing its neighbours", () => {
    mount(undefined, undefined, bar, ackOnly);
    const msg = screen.getByTestId("leaf-alarms").querySelector("[data-testid=alarm-message]") as HTMLElement;
    expect(msg.className).toContain("basis-48");
    expect(msg.className).toContain("break-words");
  });

  // #2115: Acknowledge, Snooze and the timeline toggle are one non-wrapping
  // group on one center line, not three flex items on a baseline row.
  it("renders Acknowledge, Snooze and the timeline toggle in one center-aligned group", () => {
    const withSnooze: Me = { ...ackOnly, permissions: [...ackOnly.permissions, "alarm:snooze"] };
    mount(undefined, undefined, bar, withSnooze);
    const groups = screen.getByTestId("leaf-alarms").querySelectorAll("[data-alarm-actions]");
    expect(groups).toHaveLength(1);
    const group = groups[0] as HTMLElement;
    expect(group.className).toContain("items-center");
    expect(group.className).toContain("flex-nowrap");
    expect(within(group).getByRole("button", { name: "Acknowledge" })).toBeTruthy();
    expect(within(group).getByRole("button", { name: /^Snooze/ })).toBeTruthy();
    expect(within(group).getByRole("button", { name: /^Timeline of/ })).toBeTruthy();
    // Nothing of the three sits outside the group on the row.
    expect(screen.getByTestId("leaf-alarms").querySelectorAll("button.btn-xs")).toHaveLength(group.querySelectorAll("button.btn-xs").length);
  });

  it("offers nothing to a caller without the acknowledge permission", () => {
    const readOnly: Me = { ...ackOnly, permissions: ["component:read", "system:read", "location:read"] };
    mount(undefined, undefined, bar, readOnly);
    expect(within(screen.getByTestId("leaf-alarms")).queryByRole("button", { name: "Acknowledge" })).toBeNull();
  });
});

// Two alarms on one component each carry a Snooze control (#1404): a screen
// reader's list of buttons, and a script's locator, must tell them apart by the
// alarm each acts on, not by a bare "Snooze" twice over.
describe("snoozing from Why (#1404)", () => {
  const twoAlarms = [
    { id: "al-1", component: "videobar-1", component_id: uuidFor("cf-c-bar"), severity: "high", message: "No route to host", raised_at: iso(9600), active: true, acknowledged: false },
    { id: "al-2", component: "videobar-1", component_id: uuidFor("cf-c-bar"), severity: "high", message: "Touch input lost", raised_at: iso(600), active: true, acknowledged: false },
  ];

  it("names each alarm's Snooze button for the alarm, so two are never both plainly Snooze", () => {
    mount(undefined, undefined, bar, me, twoAlarms);
    const why = screen.getByTestId("leaf-alarms");
    const names = within(why).getAllByRole("button", { name: /^Snooze/ }).map((b) => b.getAttribute("aria-label")).sort();
    expect(names).toEqual(["Snooze No route to host", "Snooze Touch input lost"]);
    expect(within(why).getByRole("button", { name: "Snooze Touch input lost" })).toBeTruthy();
  });

  it("names a snoozed alarm's Unsnooze button for the alarm too", () => {
    const snoozed = twoAlarms.map((a, i) => (i === 1 ? { ...a, snoozed_until: new Date(now + 3600_000).toISOString() } : a));
    mount(undefined, undefined, bar, me, snoozed);
    const why = screen.getByTestId("leaf-alarms");
    expect(within(why).getByRole("button", { name: "Unsnooze Touch input lost" })).toBeTruthy();
    expect(within(why).getByRole("button", { name: "Snooze No route to host" })).toBeTruthy();
  });
});

// A monitoring console's component view moves as collection arrives (#934):
// its own reads poll, so a fresh sample, a vital and an alarm land without a
// reload, each read one request however many endpoints the component has.
describe("the component leaf refreshes (#934)", () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });

  it("polls its reachability, properties, vitals, alarms and health, one request per read", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "setInterval", "clearInterval"] });
    const gets: string[] = [];
    const later = new Date().toISOString();
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = (input as Request).url;
      gets.push(new URL(url).pathname);
      if (url.includes("/reachability")) {
        return json({ component: "videobar-1", endpoints: [{ endpoint: "ssh", transport: "ssh", outpost: "edge-1", reachability: { value: "up", ts: iso(600) }, last_seen: later, layers: [], history: [] }] });
      }
      if (url.includes("/metrics")) {
        return json({ metrics: [{ metric_type_name: "uptime", label: "Uptime", data_type: "float", value: 77, is_sampled: true, from_contract: false, required: false }] });
      }
      if (url.includes("/alarms")) {
        return json({ health: "degraded", alarms: [{ id: "al-2", component: "videobar-1", component_id: uuidFor("cf-c-bar"), severity: "warning", message: "Reachability lost", raised_at: later, active: true, acknowledged: false }] });
      }
      if (url.endsWith("/properties")) {
        return json({
          properties: [
            { property_type_name: "firmware-version", value: "2.2.0", is_set: false, from_contract: true },
            // Only observed (a polled input): no value, its reading in the reconciliation, as the lab's displays read.
            { property_type_name: "video-input", label: "Video Input", data_type: "string", required: false, is_set: false, from_contract: false, reconciliation: { want: null, told: "hdmi1", is: "hdmi1", is_instance: "snmp", drift: false } },
          ],
        });
      }
      if (url.endsWith("/views/fleet")) return json(view);
      if (url.endsWith("/outposts")) return json({ outposts: [{ name: "edge-1", enrolled: true, last_heartbeat_at: later }] });
      if (url.endsWith("/outposts/edge-1")) return json({ name: "edge-1", enrolled: true, last_heartbeat_at: later });
      return json({ simulated: false });
    });
    mount();
    // Before the poll: the stale sample, no Uptime, the old alarm.
    expect(screen.getByTestId("collection-ssh").textContent).toContain("check the component or the path");
    expect(screen.getByTestId("leaf-vitals").textContent).not.toContain("Uptime");
    expect(screen.getByTestId("leaf-identity").textContent).toContain("2.1.4");

    await vi.advanceTimersByTimeAsync(10_000);

    // A property the device moved (firmware here, an input after Set input)
    // lands without a reload.
    expect(screen.getByTestId("leaf-identity").textContent).toContain("2.2.0");
    // The observed input reads as its own row: the label, then its value beside it.
    const input = [...screen.getByTestId("leaf-identity").querySelectorAll("dt")].find((dt) => dt.textContent === "Video Input");
    expect(input?.nextElementSibling?.tagName).toBe("DD");
    expect(input?.nextElementSibling?.textContent).toBe("hdmi1");

    expect(screen.getByTestId("collection-ssh").textContent).toBe("collecting");
    expect(screen.getByTestId("leaf-vitals").textContent).toContain("Uptime");
    expect(screen.getByTestId("leaf-alarms").textContent).toContain("Reachability lost");
    // The health is the component's own, off its alarms read.
    expect(screen.getByTestId("leaf-header").textContent).toContain("degraded");
    const count = (p: string) => gets.filter((g) => g.endsWith(p)).length;
    // Each read polled, as one request, not one per endpoint or per series.
    for (const p of ["/reachability", "/properties", "/metrics", "/alarms", "/outposts/edge-1"]) {
      expect(count(p)).toBeGreaterThanOrEqual(1);
      expect(count(p)).toBeLessThanOrEqual(2);
    }
    // Nothing the tab polls grows with the fleet: no fleet view, no outpost list.
    expect(count("/views/fleet")).toBe(0);
    expect(count("/outposts")).toBe(0);
  });

  // #2006: a name address redirects to the uuid; the reads keyed on the
  // resolved uuid are already cached, so the redirect must not refetch them
  // (a refetch sent the sections back to loading and collapsed them).
  it("does not refetch its reads when a name address resolves to the uuid", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async () => json({}));
    mount("/web/components/videobar-1");
    await vi.waitFor(() => expect(window.location.pathname).toBe(`/web/components/${uuidFor("cf-c-bar")}`));
    await new Promise((r) => setTimeout(r, 50));
    const paths = fetchSpy.mock.calls.map((c) => new URL((c[0] as Request).url).pathname);
    // Every read keyed on the component is already cached under its uuid.
    // (The sections the mount does not seed read once either way.)
    const seeded = ["/systems", "/reachability", "/alarms", "/properties", "/metrics"];
    expect(paths.filter((x) => x.includes("/components/") && seeded.some((p) => x.endsWith(p)))).toEqual([]);
    expect(screen.getByTestId("leaf-alarms").textContent).toContain("No route to host");
  });

  it("holds the poll while the tab is hidden", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "setInterval", "clearInterval"] });
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(async () => json({}));
    focusManager.setFocused(false);
    try {
      mount();
      // The mount's own first reads (the simulation section is not seeded)
      // are not a poll; nothing after them is.
      await vi.advanceTimersByTimeAsync(0);
      const atMount = fetchSpy.mock.calls.length;
      await vi.advanceTimersByTimeAsync(20_000);
      expect(fetchSpy.mock.calls.length).toBe(atMount);
    } finally {
      focusManager.setFocused(undefined);
    }
  });

  // A poll that returns the same data changes nothing TanStack can see, so
  // the card's ages must move with a clock of their own: an outpost that dies
  // leaves the last answer standing, and only time says it is old.
  it("ages the card with the clock when the polled data stays the same", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "setInterval", "clearInterval", "Date"] });
    const t0 = new Date().toISOString();
    const frozen = { component: "videobar-1", endpoints: [{ endpoint: "ssh", transport: "ssh", outpost: "edge-1", reachability: { value: "up", ts: t0 }, last_seen: t0, layers: [], history: [] }] };
    // Every read stays in flight: nothing the queries hold ever changes, so
    // only a clock can move the card.
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise<Response>(() => {}));
    // Seeded with exactly what every poll returns, so no read ever changes.
    mount(undefined, undefined, undefined, undefined, [], { reach: frozen as unknown as Reachability, outposts: [{ name: "edge-1", enrolled: true, last_heartbeat_at: t0, tags: {} } as Outpost] });
    expect(screen.getByTestId("collection-ssh").textContent).toBe("collecting");
    await vi.advanceTimersByTimeAsync(200_000);
    expect(screen.getByTestId("collection-ssh").textContent).toBe("outpost offline");
  });
});

// The Activity tab carries the component's own words beside its occurrences
// (#1145): its events, then its log lines, with the log filters.
describe("the leaf's activity", () => {
  it("shows the component's events and its log lines, filterable", async () => {
    mount(`/web/components/${uuidFor("cf-c-bar")}?tab=activity`);
    leafClient!.setQueryData([...EVENTS_KEY(uuidFor("cf-c-bar"))], { component: "videobar-1", events: [
      { ts: iso(30), key: "call-started", message: "Call started", provenance: "observed", origin: "caught", attributes: { call_id: "c9001" } },
    ] });
    leafClient!.setQueryData([...LOGS_KEY(uuidFor("cf-c-bar"))], { component: "videobar-1", sources: ["https"], logs: [
      { ts: iso(20), source: "https", severity: "warning", message: "uplink degraded", instance: "https" },
    ] });
    const tab = await screen.findByTestId("activity-tab");
    expect(await within(tab).findByText("Call started")).toBeTruthy();
    expect(await within(tab).findByText("uplink degraded")).toBeTruthy();
    expect(within(tab).getByLabelText("Severity at least")).toBeTruthy();
  });
});
