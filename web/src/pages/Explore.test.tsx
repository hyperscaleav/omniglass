import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library";
import { Router, Route, useLocation } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Explore from "./Explore";
import { FLEET_VIEW_KEY, type FleetView } from "../lib/fleet";
import { SYSTEMS_KEY } from "../lib/systems";
import { LOCATIONS_KEY } from "../lib/locations";
import { COMPONENTS_KEY } from "../lib/components";
import { LOCATION_TYPES_KEY } from "../lib/location_types";
import { STANDARDS_KEY } from "../lib/standards";
import { PRODUCTS_KEY } from "../lib/products";
import { COMPONENT_TYPES_KEY } from "../lib/component_types";
import { SYSTEM_TYPES_KEY } from "../lib/system_types";
import { TAGS_KEY } from "../lib/tags";
import { ME_KEY, type Me } from "../lib/auth";
import { systemHealthKey } from "../lib/health";
import { uuidFor } from "../lib/testids";

// Explore as an outline of places (#861, #867): one tree of locations, each
// place wearing the system that sits there, components directly beneath. The
// fixture is deliberately uneven: a campus of two buildings, a room holding
// two systems, a degraded room, a system at a place this caller cannot read.

const owner: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["location:read", "system:read", "component:read", "tag:read"], grants: [] };

const loc = (h: string, label: string, type: string, parent = "") => ({
  id: uuidFor(h), name: h, label, location_type: type, location_type_id: uuidFor(`t-${type}`), parent: parent ? uuidFor(parent) : undefined, verdict: "healthy",
});
const dot = (h: string, verdict = "healthy", shared = false) => ({ component: uuidFor(h), name: h, verdict, primary: true, shared });
const sys = (h: string, label: string, at: string, verdict: string, dots: ReturnType<typeof dot>[] = []) => ({
  id: uuidFor(h), name: h, label, location: at ? uuidFor(at) : undefined, verdict, dots,
});

const view = {
  locations: [
    loc("hq", "Headquarters", "campus"),
    loc("west", "West Building", "building", "hq"),
    loc("east", "East Building", "building", "hq"),
    loc("huddle", "Huddle Room", "room", "west"),
    loc("lab", "Media Lab", "room", "west"),
    loc("aud", "Auditorium", "room", "east"),
    loc("store", "Storage", "room", "east"),
    loc("depot", "Service Depot", "building"),
    loc("bay", "Bay 1", "room", "depot"),
    loc("bay2", "Bay 2", "room", "depot"),
  ],
  systems: [
    sys("s-huddle", "Huddle", "huddle", "healthy", [dot("bar-1")]),
    sys("s-class", "Classroom", "lab", "healthy", [dot("cam-1")]),
    sys("s-class2", "Classroom 2", "lab", "incomplete", [dot("disp-2", "incomplete")]),
    sys("s-aud", "Auditorium AV", "aud", "degraded", [dot("dsp-1", "outage"), dot("mic-1")]),
    sys("s-bay", "Bay AV", "bay", "healthy", [dot("bay-dsp")]),
    sys("s-hidden", "Hidden AV", "elsewhere", "healthy"),
  ],
} as unknown as FleetView;

const comp = (h: string, product: string, system: string, at: string) => ({
  id: uuidFor(h), name: h, label: "", product, system_id: uuidFor(system), system_count: 1, location_id: uuidFor(at), actions: ["update", "delete"],
});
const components = [
  comp("bar-1", "lyra-bar", "s-huddle", "huddle"),
  comp("cam-1", "kestrel", "s-class", "lab"),
  comp("disp-2", "boreal", "s-class2", "lab"),
  comp("dsp-1", "polaris-dsp", "s-aud", "aud"),
  comp("mic-1", "lyra-mic", "s-aud", "aud"),
  comp("bay-dsp", "polaris-dsp", "s-bay", "bay"),
];
const types = [
  { id: uuidFor("t-campus"), name: "campus", label: "Campus", icon: "landmark", official: true, forked: false, allowed_parent_types: ["root"] },
  { id: uuidFor("t-building"), name: "building", label: "Building", icon: "building", official: true, forked: false, allowed_parent_types: ["root", "campus"] },
  { id: uuidFor("t-room"), name: "room", label: "Room", icon: "door-open", official: true, forked: false, allowed_parent_types: ["building"] },
];

function mount(path = "/web/explore", me: Me = owner, fleet: FleetView | "unread" = view, keepOpen = false, comps: unknown[] = components, seed?: (qc: QueryClient) => void) {
  if (!keepOpen) localStorage.removeItem("explore-open");
  const source = fleet === "unread" ? view : fleet;
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  if (fleet !== "unread") qc.setQueryData([...FLEET_VIEW_KEY], fleet);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...SYSTEMS_KEY], (source.systems ?? []).map((s) => ({
    id: s.id, name: s.name, label: s.label, location_id: s.location, member_count: 1, actions: ["update", "delete"],
    standard: s.name === "s-aud" ? "auditorium" : s.name === "s-huddle" ? "huddle-room" : "",
    effective_tags: s.name === "s-huddle" ? { environment: "staging" } : {},
  })));
  qc.setQueryData([...LOCATIONS_KEY], (source.locations ?? []).map((l) => ({ id: l.id, name: l.name, label: l.label, location_type: l.location_type, parent_id: l.parent, actions: ["update", "delete"], effective_tags: {} })));
  qc.setQueryData([...COMPONENTS_KEY], fleet === "unread" ? [] : comps);
  qc.setQueryData([...LOCATION_TYPES_KEY], types);
  qc.setQueryData([...STANDARDS_KEY], [{ id: uuidFor("std-a"), name: "auditorium", label: "Auditorium" }, { id: uuidFor("std-h"), name: "huddle-room", label: "Huddle Room" }]);
  qc.setQueryData([...PRODUCTS_KEY], [{ id: uuidFor("p1"), name: "polaris-dsp", label: "Polaris DSP 16", component_type: "dsp" }]);
  qc.setQueryData([...COMPONENT_TYPES_KEY], []);
  qc.setQueryData([...SYSTEM_TYPES_KEY], []);
  qc.setQueryData([...TAGS_KEY], []);
  seed?.(qc);
  window.history.pushState({}, "", path);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router base="/web">
        <Route path="/explore" component={Explore} />
        <Route path="/:kind/create" component={() => { const l = useLocation(); return <div data-testid="create-page">{l.pathname + l.search}</div>; }} />
      </Router>
    </QueryClientProvider>
  ));
}

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const tree = () => screen.getByRole("tree");
const rows = () => within(tree()).getAllByRole("treeitem");
const labelOf = (r: HTMLElement) => r.querySelector("[data-label]")?.textContent ?? "";
const row = (label: string) => {
  const r = rows().find((x) => labelOf(x) === label);
  if (!r) throw new Error(`no row ${label}; rows are ${rows().map(labelOf).join(", ")}`);
  return r;
};
const rowLabels = () => rows().map(labelOf);
const expand = (label: string) => fireEvent.click(within(row(label)).getByRole("button", { name: `Expand ${label}` }));

describe("arrival", () => {
  it("shows the top-level places only, sorted by name, collapsed", async () => {
    mount();
    await screen.findByRole("tree");
    expect(rowLabels()).toEqual(["Headquarters", "Service Depot", "Placed nowhere you can see"]);
    expect(row("Headquarters").getAttribute("aria-expanded")).toBe("false");
  });

  it("says what a collapsed place holds in the registry's words, and counts its systems by health", async () => {
    mount();
    await screen.findByRole("tree");
    const hq = row("Headquarters");
    expect(hq.textContent).toContain("Campus");
    expect(hq.textContent).toContain("2 buildings");
    expect(within(hq).getByTestId("health").getAttribute("aria-label")).toBe("2 healthy, 1 incomplete, 1 degraded");
  });

  it("gives only trouble a hue: a healthy count stays grey", async () => {
    mount();
    await screen.findByRole("tree");
    const slots = within(row("Headquarters")).getByTestId("health");
    expect(slots.querySelector("[data-slot=healthy]")!.className).not.toMatch(/text-(success|warning|error|incomplete)/);
    expect(slots.querySelector("[data-slot=degraded]")!.className).toContain("text-warning");
    expect(slots.querySelector("[data-slot=incomplete]")!.className).toContain("text-incomplete");
  });
});

describe("drilling in", () => {
  it("expands and collapses a place", async () => {
    mount();
    await screen.findByRole("tree");
    expand("Headquarters");
    expect(rowLabels()).toContain("West Building");
    fireEvent.click(within(row("Headquarters")).getByRole("button", { name: "Collapse Headquarters" }));
    expect(rowLabels()).not.toContain("West Building");
  });

  it("remembers what is open in this browser", async () => {
    mount();
    await screen.findByRole("tree");
    expand("Headquarters");
    cleanup();
    mount("/web/explore", owner, view, true);
    await screen.findByRole("tree");
    expect(row("Headquarters").getAttribute("aria-expanded")).toBe("true");
    expect(rowLabels()).toContain("West Building");
  });

  it("wears a place's system on its row and lists the components directly beneath", async () => {
    mount();
    await screen.findByRole("tree");
    expand("Headquarters");
    expand("East Building");
    const aud = row("Auditorium");
    expect(aud.textContent).toContain("2 components");
    expect(within(aud).getByTestId("standard").textContent).toBe("Auditorium");
    expand("Auditorium");
    expect(rowLabels()).toEqual(expect.arrayContaining(["dsp-1", "mic-1"]));
    expect(rowLabels()).not.toContain("Auditorium AV");
  });

  it("groups a place's components under one header per system when it holds two", async () => {
    mount();
    await screen.findByRole("tree");
    expand("Headquarters");
    expand("West Building");
    expand("Media Lab");
    const labels = rowLabels();
    expect(labels.indexOf("Classroom")).toBeLessThan(labels.indexOf("cam-1"));
    expect(labels.indexOf("Classroom 2")).toBeLessThan(labels.indexOf("disp-2"));
  });

  it("expands a whole branch at once with Alt", async () => {
    mount();
    await screen.findByRole("tree");
    fireEvent.click(within(row("Headquarters")).getByRole("button", { name: "Expand Headquarters" }), { altKey: true });
    expect(rowLabels()).toEqual(expect.arrayContaining(["West Building", "Huddle Room", "bar-1", "Auditorium", "dsp-1"]));
  });

  it("moves through rows and opens them from the keyboard", async () => {
    mount();
    await screen.findByRole("tree");
    row("Headquarters").focus();
    fireEvent.keyDown(row("Headquarters"), { key: "ArrowRight" });
    expect(row("Headquarters").getAttribute("aria-expanded")).toBe("true");
    fireEvent.keyDown(row("Headquarters"), { key: "ArrowDown" });
    expect(document.activeElement).toBe(row("East Building"));
    fireEvent.keyDown(row("East Building"), { key: "ArrowLeft" });
    expect(document.activeElement).toBe(row("Headquarters"));
    fireEvent.keyDown(row("Headquarters"), { key: "ArrowLeft" });
    expect(row("Headquarters").getAttribute("aria-expanded")).toBe("false");
  });
});

describe("the side panel", () => {
  it("opens a place holding one system as that system", async () => {
    mount();
    await screen.findByRole("tree");
    expand("Headquarters");
    expand("West Building");
    fireEvent.click(row("Huddle Room"));
    const blade = await screen.findByRole("dialog");
    expect(blade.getAttribute("aria-labelledby")).toBe(`blade-title-system-${uuidFor("s-huddle")}`);
  });

  it("opens any other place as the location", async () => {
    mount();
    await screen.findByRole("tree");
    fireEvent.click(row("Headquarters"));
    expect((await screen.findByRole("dialog")).getAttribute("aria-labelledby")).toBe(`blade-title-location-${uuidFor("hq")}`);
  });

  it("opens a component as itself", async () => {
    mount();
    await screen.findByRole("tree");
    expand("Headquarters");
    expand("East Building");
    expand("Auditorium");
    fireEvent.click(row("dsp-1"));
    expect((await screen.findByRole("dialog")).getAttribute("aria-labelledby")).toBe(`blade-title-component-${uuidFor("dsp-1")}`);
  });
});

describe("a place with nothing beneath", () => {
  it("offers no chevron when its system holds no components yet", async () => {
    const fleet = { locations: [loc("quiet", "Quiet Room", "room")], systems: [sys("s-quiet", "Quiet", "quiet", "healthy")] } as unknown as FleetView;
    mount("/web/explore", owner, fleet, false, []);
    await screen.findByRole("tree");
    expect(within(row("Quiet Room")).queryByRole("button", { name: "Expand Quiet Room" })).toBeNull();
    expect(row("Quiet Room").getAttribute("aria-expanded")).toBeNull();
  });
});

describe("an empty fleet", () => {
  it("still shows a component placed nowhere, rather than claiming there is nothing", async () => {
    const fleet = { locations: [], systems: [] } as unknown as FleetView;
    mount("/web/explore", owner, fleet, false, [{ id: uuidFor("stray"), name: "stray", label: "", product: "lyra-bar", system_count: 0, actions: ["update", "delete"] }]);
    await screen.findByRole("tree");
    expect(rowLabels()).toEqual(["Placed nowhere you can see"]);
    expect(screen.queryByText("No locations yet.")).toBeNull();
  });
});

describe("creating where you stand", () => {
  it("offers only what the type rules let sit under a row, placed there", async () => {
    mount();
    await screen.findByRole("tree");
    expand("Headquarters");
    fireEvent.click(within(row("West Building")).getByRole("button", { name: "Add under West Building" }));
    expect(await screen.findByRole("button", { name: "Location" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "System" }));
    expect((await screen.findByTestId("create-page")).textContent).toBe(`/web/systems/create?under=${uuidFor("west")}`);
  });

  it("does not offer a location under a type nothing may sit under", async () => {
    mount();
    await screen.findByRole("tree");
    expand("Headquarters");
    expand("East Building");
    fireEvent.click(within(row("Storage")).getByRole("button", { name: "Add under Storage" }));
    expect(await screen.findByRole("button", { name: "System" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Location" })).toBeNull();
  });

  it("offers each place of a folded row, so the outer one stays reachable", async () => {
    // A campus of one building folds into one row; the operator must still be
    // able to add a second building beside the first, under the campus.
    const fleet = { locations: [loc("annex", "Annex Campus", "campus"), loc("annex-b", "Annex", "building", "annex")], systems: [] } as unknown as FleetView;
    mount("/web/explore", owner, fleet);
    await screen.findByRole("tree");
    fireEvent.click(within(rows()[0]).getByRole("button", { name: "Add under Annex Campus / Annex" }));
    const outer = await screen.findByRole("group", { name: "Under Annex Campus" });
    expect(within(screen.getByRole("group", { name: "Under Annex" })).getByRole("button", { name: "System" })).toBeTruthy();
    fireEvent.click(within(outer).getByRole("button", { name: "Location" }));
    expect((await screen.findByTestId("create-page")).textContent).toBe(`/web/locations/create?under=${uuidFor("annex")}`);
  });

  it("offers nothing to a caller who may not create", async () => {
    mount("/web/explore", viewer);
    await screen.findByRole("tree");
    expect(within(row("Headquarters")).queryByRole("button", { name: "Add under Headquarters" })).toBeNull();
    expect(screen.queryByRole("button", { name: "New" })).toBeNull();
  });
});

describe("finding things", () => {
  const filterInput = () => screen.getByPlaceholderText(/^Filter by/) as HTMLInputElement;
  const results = () => within(screen.getByRole("list", { name: "Matches" })).getAllByRole("listitem");

  it("narrows to what needs attention, each with its path", async () => {
    mount();
    await screen.findByRole("tree");
    const button = within(screen.getByTestId("explore-counts")).getByRole("button", { name: /need attention/ });
    expect(button.textContent).toContain("2");
    fireEvent.click(button);
    await waitFor(() => expect(screen.queryByRole("tree")).toBeNull());
    const texts = results().map((r) => r.textContent ?? "");
    expect(texts.some((t) => t.includes("Headquarters / East Building / ") && t.includes("Auditorium"))).toBe(true);
    expect(texts.some((t) => t.includes("dsp-1"))).toBe(true);
    expect(texts.some((t) => t.includes("Huddle Room"))).toBe(false);
  });

  it("matches a standard, a product and a tag", async () => {
    mount();
    await screen.findByRole("tree");
    for (const [term, expected] of [["standard:Auditorium", "Auditorium"], ["product:Polaris DSP 16", "dsp-1"], ["environment:staging", "Huddle Room"]] as const) {
      fireEvent.input(filterInput(), { target: { value: term } });
      fireEvent.keyDown(filterInput(), { key: "Enter" });
      await waitFor(() => expect(results().some((r) => (r.textContent ?? "").includes(expected))).toBe(true));
      fireEvent.click(screen.getByRole("button", { name: "Clear" }));
      await screen.findByRole("tree");
    }
  });

  it("reveals a result in the tree on Enter, expanded down to it and selected", async () => {
    mount();
    await screen.findByRole("tree");
    fireEvent.input(filterInput(), { target: { value: "dsp-1" } });
    fireEvent.keyDown(filterInput(), { key: "Enter" });
    const hit = await waitFor(() => {
      const h = results().find((r) => (r.textContent ?? "").includes("dsp-1"));
      if (!h) throw new Error("no hit yet");
      return h;
    });
    fireEvent.keyDown(hit, { key: "Enter" });
    await screen.findByRole("tree");
    expect(row("dsp-1").getAttribute("aria-selected")).toBe("true");
    expect(row("Auditorium").getAttribute("aria-expanded")).toBe("true");
  });

  it("reveals the row a ?node= link names, by id or by a unique name", async () => {
    mount(`/web/explore?node=${uuidFor("aud")}`);
    await screen.findByRole("tree");
    await waitFor(() => expect(row("Auditorium").getAttribute("aria-selected")).toBe("true"));
    cleanup();
    mount("/web/explore?node=bay");
    await screen.findByRole("tree");
    await waitFor(() => expect(row("Bay 1").getAttribute("aria-selected")).toBe("true"));
  });

  it("says so when a filter matches nothing", async () => {
    mount();
    await screen.findByRole("tree");
    fireEvent.input(filterInput(), { target: { value: "nothing-is-called-this" } });
    fireEvent.keyDown(filterInput(), { key: "Enter" });
    expect(await screen.findByText("Nothing matches the filter.")).toBeTruthy();
  });
});

describe("the states an operator can land in", () => {
  it("collects systems at a place this caller cannot read under their own node", async () => {
    mount();
    await screen.findByRole("tree");
    expand("Placed nowhere you can see");
    expect(rowLabels()).toContain("Hidden AV");
  });

  it("teaches the empty fleet and offers the first location to somebody who may create one", async () => {
    mount("/web/explore", owner, { locations: [], systems: [] } as unknown as FleetView);
    expect(await screen.findByText("No locations yet.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "New location" })).toBeTruthy();
    cleanup();
    mount("/web/explore", viewer, { locations: [], systems: [] } as unknown as FleetView);
    expect(await screen.findByText("No locations yet.")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "New location" })).toBeNull();
  });

  it("offers a retry when the fleet cannot be read, and reads again when asked", async () => {
    const fetcher = vi.fn(() => Promise.reject(new Error("the wire is down")));
    vi.stubGlobal("fetch", fetcher);
    mount("/web/explore", owner, "unread");
    expect(await screen.findByRole("alert")).toBeTruthy();
    const before = fetcher.mock.calls.length;
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await waitFor(() => expect(fetcher.mock.calls.length).toBeGreaterThan(before));
  });
});

describe("the Detail column", () => {
  it("names the alarm behind a component's red light, matched by the component's id", async () => {
    // A health report names an alarm's component by uuid (internal/api/health.go).
    const report = { roles: [{ alarms: [{ id: "a1", component: uuidFor("dsp-1"), severity: "critical", message: "No signal on input 3" }] }] };
    mount("/web/explore", owner, view, false, components, (qc) => qc.setQueryData([...systemHealthKey(uuidFor("s-aud"))], report));
    await screen.findByRole("tree");
    expand("Headquarters");
    expand("East Building");
    expand("Auditorium");
    await waitFor(() => expect(row("dsp-1").textContent).toContain("No signal on input 3"));
  });

  it("reads no health report while the filter shows results instead of the tree", async () => {
    localStorage.setItem("explore-open", JSON.stringify([uuidFor("hq"), uuidFor("east"), uuidFor("aud")]));
    const health = vi.fn(async (_u: RequestInfo | URL) => new Response(JSON.stringify({ roles: [] }), { status: 200, headers: { "content-type": "application/json" } }));
    vi.stubGlobal("fetch", health);
    mount(`/web/explore?chips=${encodeURIComponent(JSON.stringify([{ key: "name", op: "contains", values: ["dsp"] }]))}`, owner, view, true);
    await screen.findByRole("list", { name: "Matches" });
    expect(health.mock.calls.filter(([u]) => String(u instanceof Request ? u.url : u).includes("/health"))).toHaveLength(0);
  });
});

describe("a place's health light", () => {
  it("counts the places beneath it too, so a collapsed branch cannot hide an outage", async () => {
    const fleet = {
      locations: [loc("annex", "Annex", "building"), loc("vault", "Vault", "room", "annex"), loc("vault2", "Vault 2", "room", "annex")],
      systems: [sys("s-lobby", "Lobby AV", "annex", "healthy"), sys("s-vault", "Vault AV", "vault", "outage")],
    } as unknown as FleetView;
    mount("/web/explore", owner, fleet, false, []);
    await screen.findByRole("tree");
    expect(within(row("Annex")).getByTestId("health").getAttribute("aria-label")).toBe("1 healthy, 1 outage");
  });
});

describe("keyboard", () => {
  it("keeps focus on the same row when it expands", async () => {
    mount();
    await screen.findByRole("tree");
    const hq = row("Headquarters");
    hq.focus();
    fireEvent.keyDown(hq, { key: "ArrowRight" });
    expect(row("Headquarters").getAttribute("aria-expanded")).toBe("true");
    expect(hq.isConnected).toBe(true);
    expect(document.activeElement).toBe(hq);
  });

  it("opens a row's add menu with +", async () => {
    mount();
    await screen.findByRole("tree");
    expand("Headquarters");
    row("West Building").focus();
    fireEvent.keyDown(row("West Building"), { key: "+" });
    expect(await screen.findByRole("group", { name: "Under West Building" })).toBeTruthy();
  });

  it("leaves a key pressed inside the add menu to the menu, not the row behind it", async () => {
    mount();
    await screen.findByRole("tree");
    expand("Headquarters");
    fireEvent.click(within(row("West Building")).getByRole("button", { name: "Add under West Building" }));
    const system = await screen.findByRole("button", { name: "System" });
    fireEvent.keyDown(system, { key: "Enter" });
    // The menu is a dialog of its own; no side panel opened behind it.
    expect(screen.queryAllByRole("dialog").filter((d) => d.getAttribute("aria-labelledby")?.startsWith("blade-title"))).toHaveLength(0);
  });

  it("opens a filter result's side panel with Space", async () => {
    mount();
    await screen.findByRole("tree");
    fireEvent.input(screen.getByPlaceholderText(/^Filter by/), { target: { value: "dsp-1" } });
    fireEvent.keyDown(screen.getByPlaceholderText(/^Filter by/), { key: "Enter" });
    const hit = await waitFor(() => {
      const h = within(screen.getByRole("list", { name: "Matches" })).getAllByRole("listitem").find((r) => (r.textContent ?? "").includes("dsp-1"));
      if (!h) throw new Error("no hit yet");
      return h;
    });
    fireEvent.keyDown(hit, { key: " " });
    expect((await screen.findByRole("dialog")).getAttribute("aria-labelledby")).toBe(`blade-title-component-${uuidFor("dsp-1")}`);
  });
});

describe("what a link or a result points at", () => {
  const hidden = {
    locations: [loc("annex", "Annex Campus", "campus"), loc("annex-b", "Annex", "building", "annex")],
    systems: [sys("s-h1", "Hidden One", "elsewhere", "degraded"), sys("s-h2", "Hidden Two", "", "healthy")],
  } as unknown as FleetView;

  it("reveals the node of things placed out of sight, rather than opening a place that is not one", async () => {
    mount("/web/explore", owner, hidden, false, []);
    await screen.findByRole("tree");
    fireEvent.input(screen.getByPlaceholderText(/^Filter by/), { target: { value: "Placed nowhere" } });
    fireEvent.keyDown(screen.getByPlaceholderText(/^Filter by/), { key: "Enter" });
    const hit = await waitFor(() => {
      const h = within(screen.getByRole("list", { name: "Matches" })).getAllByRole("listitem").find((r) => (r.textContent ?? "").includes("Placed nowhere"));
      if (!h) throw new Error("no hit yet");
      return h;
    });
    fireEvent.click(hit);
    await screen.findByRole("tree");
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(row("Placed nowhere you can see").getAttribute("aria-selected")).toBe("true");
  });

  it("selects a folded row when a link names its outer place", async () => {
    mount(`/web/explore?node=${uuidFor("annex")}`, owner, hidden, false, []);
    await screen.findByRole("tree");
    await waitFor(() => expect(row("Annex").getAttribute("aria-selected")).toBe("true"));
  });

  it("opens the node of things placed out of sight to reveal a system linked there", async () => {
    mount(`/web/explore?node=${uuidFor("s-h2")}`, owner, hidden, false, []);
    await screen.findByRole("tree");
    await waitFor(() => expect(row("Hidden Two").getAttribute("aria-selected")).toBe("true"));
  });

  it("reveals nothing when a name names two things of different kinds", async () => {
    const twin = { locations: [loc("annex", "Annex Campus", "campus")], systems: [sys("annex2", "Annex AV", "annex", "healthy")] } as unknown as FleetView;
    // A system that shares the location's name.
    (twin.systems as unknown as { name: string }[])[0].name = "annex";
    mount("/web/explore?node=annex", owner, twin, false, []);
    await screen.findByRole("tree");
    await new Promise((r) => setTimeout(r, 20));
    expect(rows().filter((r) => r.getAttribute("aria-selected") === "true")).toHaveLength(0);
  });
});
