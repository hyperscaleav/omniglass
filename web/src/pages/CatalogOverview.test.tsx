import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, waitFor, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import CatalogOverview from "./CatalogOverview";
import { CATALOG_GROUPS } from "../lib/catalog";
import { ME_KEY, type Me } from "../lib/auth";
import { PRODUCTS_KEY } from "../lib/products";
import { VENDORS_KEY } from "../lib/vendors";
import { DRIVERS_KEY } from "../lib/drivers";
import { CAPABILITIES_KEY } from "../lib/capabilities";
import { STANDARDS_KEY } from "../lib/standards";
import { LOCATION_TYPES_KEY } from "../lib/location_types";
import { METRICS_KEY } from "../lib/metric_types";
import { PROPERTIES_KEY } from "../lib/properties";
import { EVENT_TYPES_KEY } from "../lib/event_types";
import { COMMAND_TYPES_KEY } from "../lib/command_types";
import { TAGS_KEY } from "../lib/tags";

// The Catalog overview (/catalog): the landing the shell's Overview entry
// opens. One card per catalog group, DERIVED from the SAME CATALOG_GROUPS table
// the CatalogShell subrail renders (one source of truth), so membership, order,
// copy, and gating can never drift between the two surfaces: a group whose
// entries are all gated away renders no card. Counts ride the list pages' own
// query keys (learning-tool doctrine: real rows, not decoration); each registry
// is its own query, so one failing fetch marks its own row and nothing else
// (#598). A soon entry reads soon without a count; a routed soon entry (Rules,
// Notifications) links to its stub, a pageless one (Templates) does not link.

const owner: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "view" }, permissions: ["*:read"], grants: [] };
const meWith = (permissions: string[]): Me => ({ principal: { id: "u-x", kind: "human" }, permissions, grants: [] });

// Rows only need a length: the page renders counts, never row fields.
const rows = (n: number) => Array.from({ length: n }, (_, i) => ({ name: `row-${i}` }));

// Every registry's shared cache key, primed with a distinct count so a card's
// numbers are tellable apart, keyed exactly as the list pages key them.
const PRIMED: [readonly string[], number][] = [
  [PRODUCTS_KEY, 2],
  [VENDORS_KEY, 1],
  [DRIVERS_KEY, 8],
  [CAPABILITIES_KEY, 3],
  [STANDARDS_KEY, 4],
  [LOCATION_TYPES_KEY, 5],
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
const entry = (cardEl: HTMLElement, label: string) => within(cardEl).getByText(label).closest("[data-entry]") as HTMLElement;

describe("CatalogOverview page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders one card per group, derived from the shared table, in its order, with live counts", () => {
    mount();
    expect([...document.querySelectorAll("[data-card]")].map((el) => el.getAttribute("data-card"))).toEqual(
      CATALOG_GROUPS.map((g) => g.header),
    );
    const comp = card("Components")!;
    expect(entry(comp, "Vendors").textContent).toContain("1");
    expect(entry(comp, "Products").textContent).toContain("2");
    expect(entry(comp, "Drivers").textContent).toContain("8");
    expect(entry(comp, "Capabilities").textContent).toContain("3");
    expect(entry(card("Telemetry")!, "Metrics").textContent).toContain("12");
    expect(entry(card("Systems")!, "Standards").textContent).toContain("4");
    expect(entry(card("Locations")!, "Types").textContent).toContain("5");
    expect(entry(card("Metadata")!, "Tags").textContent).toContain("9");
  });

  it("teaches each group with the table's own sentence", () => {
    mount();
    for (const g of CATALOG_GROUPS) {
      expect(within(card(g.header)!).getByText(g.copy)).toBeTruthy();
    }
  });

  it("links each live entry to its canonical route", () => {
    mount();
    expect(entry(card("Components")!, "Products").getAttribute("href")).toBe("/products");
    expect(entry(card("Telemetry")!, "Metrics").getAttribute("href")).toBe("/metrics");
    expect(entry(card("Locations")!, "Types").getAttribute("href")).toBe("/location-types");
    expect(entry(card("Metadata")!, "Tags").getAttribute("href")).toBe("/tags");
  });

  it("offers no secret-types row anywhere, per the standing ruling", () => {
    mount();
    expect(document.querySelector('[data-entry] [href="/secret-types"]')).toBeNull();
    expect(document.querySelector('a[href="/secret-types"]')).toBeNull();
  });

  it("renders soon entries without counts: the routed ones link to their stubs, the pageless ones do not link", () => {
    mount();
    const act = card("Actions")!;
    expect(entry(act, "Commands").textContent).toContain("6");
    const rules = entry(act, "Rules");
    expect(rules.textContent).toContain("soon");
    expect(rules.textContent).not.toMatch(/\d/);
    expect(rules.getAttribute("href")).toBe("/rules");
    expect(entry(act, "Notifications").getAttribute("href")).toBe("/notifications");
    // A pageless soon slot (Templates) reads soon and is not an anchor.
    const templates = entry(card("Components")!, "Templates");
    expect(templates.textContent).toContain("soon");
    expect(templates.tagName).not.toBe("A");
  });

  it("keeps every card for a *:read floor viewer", () => {
    mount(viewer);
    expect(document.querySelectorAll("[data-card]")).toHaveLength(CATALOG_GROUPS.length);
  });

  it("hides a fully gated group's card and never creates the gated query", () => {
    const { qc } = mount(meWith(["tag:read"]), { skip: [METRICS_KEY, PROPERTIES_KEY, EVENT_TYPES_KEY] });
    // Telemetry's entries are all gated away: no card, and no query was ever
    // created for its registries (the structural #598 fact, synchronous).
    expect(card("Telemetry")).toBeNull();
    expect(card("Metadata")).toBeTruthy();
    expect(entry(card("Metadata")!, "Tags").textContent).toContain("9");
    expect(qc.getQueryCache().find({ queryKey: [...METRICS_KEY] })).toBeUndefined();
  });

  it("shows a loading skeleton for a count still in flight without blocking the others", () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
    mount(owner, { skip: [DRIVERS_KEY] });
    const comp = card("Components")!;
    expect(entry(comp, "Drivers").querySelector(".skeleton")).toBeTruthy();
    expect(entry(comp, "Products").textContent).toContain("2");
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
    await waitFor(() => expect(entry(comp, "Vendors").textContent).toContain("unavailable"));
    expect(entry(comp, "Products").textContent).toContain("2");
    expect(entry(card("Metadata")!, "Tags").textContent).toContain("9");
  });
});
