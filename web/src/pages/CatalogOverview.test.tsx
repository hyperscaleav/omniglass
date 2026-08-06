import { describe, it, expect, vi, afterEach } from "vitest";
import { render, waitFor, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import CatalogOverview, { REGISTRIES, SECTION_COPY } from "./CatalogOverview";
import { ME_KEY, type Me } from "../lib/auth";
import { navItems } from "../lib/nav";
import { PRODUCTS_KEY } from "../lib/products";
import { VENDORS_KEY } from "../lib/vendors";
import { DRIVERS_KEY } from "../lib/drivers";
import { CAPABILITIES_KEY } from "../lib/capabilities";
import { STANDARDS_KEY } from "../lib/standards";
import { LOCATION_TYPES_KEY } from "../lib/location_types";
import { SECRET_TYPES_KEY } from "../lib/secret_types";
import { METRICS_KEY } from "../lib/metric_types";
import { PROPERTIES_KEY } from "../lib/properties";
import { EVENT_TYPES_KEY } from "../lib/event_types";
import { COMMAND_TYPES_KEY } from "../lib/command_types";
import { TAGS_KEY } from "../lib/tags";

// The Catalog overview (/catalog, #609): one card per Catalog section, derived
// from the same navItems the sidebar renders, each card teaching its section and
// showing LIVE registry counts (learning-tool doctrine: real data, not
// decoration). Visibility reuses filterNav over the same can() the sidebar uses,
// so a section fully gated away renders no card; each registry is its own query,
// so one failing fetch cannot take the other cards down (#598).

const owner: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "view" }, permissions: ["*:read"], grants: [] };

// Rows only need a length: the page renders counts, never row fields.
const rows = (n: number) => Array.from({ length: n }, (_, i) => ({ name: `row-${i}` }));

// Every live registry's shared cache key, primed with a distinct count so a
// card's numbers are tellable apart, keyed exactly as the list pages key them
// (a shared cache, not a parallel one).
const PRIMED: [readonly string[], number][] = [
  [PRODUCTS_KEY, 2],
  [VENDORS_KEY, 1],
  [DRIVERS_KEY, 8],
  [CAPABILITIES_KEY, 3],
  [STANDARDS_KEY, 4],
  [LOCATION_TYPES_KEY, 5],
  [SECRET_TYPES_KEY, 2],
  [METRICS_KEY, 12],
  [PROPERTIES_KEY, 7],
  [EVENT_TYPES_KEY, 15],
  [COMMAND_TYPES_KEY, 6],
  [TAGS_KEY, 9],
];

function mount(me: Me = owner, opts: { skip?: (readonly string[])[] } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], me);
  for (const [key, n] of PRIMED) {
    if (opts.skip?.some((s) => s.join("/") === key.join("/"))) continue;
    qc.setQueryData([...key], rows(n));
  }
  const result = render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={CatalogOverview} />
      </Router>
    </QueryClientProvider>
  ));
  return { ...result, qc };
}

const card = (label: string) => document.querySelector(`[data-card="${label}"]`) as HTMLElement | null;
const entryRow = (cardEl: HTMLElement, label: string) => within(cardEl).getByText(label).closest("a") as HTMLElement;

describe("CatalogOverview page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders one card per Catalog section, in nav order, with live counts", () => {
    mount();
    expect([...document.querySelectorAll("[data-card]")].map((el) => el.getAttribute("data-card"))).toEqual([
      "Components", "Systems", "Locations", "Secrets", "Telemetry", "Action", "General",
    ]);
    const comp = card("Components")!;
    expect(entryRow(comp, "Products").textContent).toContain("2");
    expect(entryRow(comp, "Vendors").textContent).toContain("1");
    expect(entryRow(comp, "Drivers").textContent).toContain("8");
    expect(entryRow(comp, "Capabilities").textContent).toContain("3");
    expect(entryRow(card("Systems")!, "Standards").textContent).toContain("4");
    expect(entryRow(card("Locations")!, "Types").textContent).toContain("5");
    expect(entryRow(card("Secrets")!, "Types").textContent).toContain("2");
    expect(entryRow(card("General")!, "Tags").textContent).toContain("9");
  });

  it("links each entry to its page and teaches the section", () => {
    mount();
    const comp = card("Components")!;
    expect(entryRow(comp, "Products").getAttribute("href")).toBe("/products");
    expect(entryRow(card("General")!, "Tags").getAttribute("href")).toBe("/tags");
    // The teaching copy, present tense, on the card body.
    expect(within(comp).getByText(/points at a product/)).toBeTruthy();
    expect(within(card("Systems")!).getByText(/blueprint a system conforms to/)).toBeTruthy();
  });

  // The same permission logic as the sidebar (filterNav over can()): secret is a
  // sensitive resource off the *:read floor, so the floor viewer loses the whole
  // Secrets section, card and fetch alike, and keeps every other card (#598).
  // The leak pin asserts on the query cache, not fetch timing: the client's
  // async onRequest middleware makes any fetch a microtask away, so a
  // synchronous not-called spy could never fail (the vacuous-async-spy trap).
  // "The query was never created" is the structural fact, and it is synchronous.
  it("shows a *:read floor viewer no Secrets card and never creates the gated query", () => {
    const { qc } = mount(viewer, { skip: [SECRET_TYPES_KEY] });
    expect(card("Secrets")).toBeNull();
    expect(card("Components")).toBeTruthy();
    expect(card("Locations")).toBeTruthy();
    expect(entryRow(card("Locations")!, "Types").textContent).toContain("5");
    expect(qc.getQueryCache().find({ queryKey: [...SECRET_TYPES_KEY] })).toBeUndefined();
  });

  it("renders soon entries as chips without counts, beside live counts", () => {
    mount();
    const tel = card("Telemetry")!;
    expect(entryRow(tel, "Metrics").textContent).toContain("12");
    const logs = entryRow(tel, "Logs");
    expect(logs.textContent).toContain("soon");
    expect(logs.textContent).not.toMatch(/\d/);
    const act = card("Action")!;
    expect(entryRow(act, "Rules").textContent).toContain("soon");
    expect(entryRow(act, "Notifications").textContent).toContain("soon");
    expect(entryRow(act, "Commands").textContent).toContain("6");
  });

  it("shows a loading skeleton for a count still in flight without blocking the others", () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
    mount(owner, { skip: [DRIVERS_KEY] });
    const comp = card("Components")!;
    expect(entryRow(comp, "Drivers").querySelector(".skeleton")).toBeTruthy();
    expect(entryRow(comp, "Products").textContent).toContain("2");
  });

  // One query per registry: a failing fetch marks its own row and leaves every
  // other count standing (the #598 lesson; no all-or-nothing Promise.all).
  it("marks a failing registry unavailable while other cards keep their counts", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = typeof input === "string" ? input : (input as Request).url;
      if (url.includes("/vendors")) {
        return new Response(JSON.stringify({ title: "boom" }), { status: 500, headers: { "Content-Type": "application/json" } });
      }
      throw new Error(`unexpected fetch ${url}`);
    });
    mount(owner, { skip: [VENDORS_KEY] });
    const comp = card("Components")!;
    await waitFor(() => expect(entryRow(comp, "Vendors").textContent).toContain("unavailable"));
    expect(entryRow(comp, "Products").textContent).toContain("2");
    expect(entryRow(card("General")!, "Tags").textContent).toContain("9");
  });
});

// The two hand-authored maps are the page's only couplings to the nav; this
// invariant keeps them self-policing. Without it, a future live entry with no
// REGISTRIES mapping renders "soon" on the hub while the rail shows it live,
// and a new section without copy renders an empty teaching paragraph, both
// silently.
describe("hub maps track the nav (#609)", () => {
  const catalogChildren = navItems.find((i) => i.label === "Catalog")!.children!;
  it("maps every live sectioned Catalog entry to a registry", () => {
    for (const c of catalogChildren.filter((c) => c.section && c.live))
      expect(REGISTRIES[c.path], `${c.section} > ${c.label} (${c.path}) has no REGISTRIES entry`).toBeTruthy();
  });
  it("gives every Catalog section its teaching copy", () => {
    for (const s of new Set(catalogChildren.map((c) => c.section).filter(Boolean)))
      expect(SECTION_COPY[s!], `section ${s} has no SECTION_COPY entry`).toBeTruthy();
  });
});
