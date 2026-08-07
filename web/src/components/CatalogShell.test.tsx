import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import CatalogShell from "./CatalogShell";
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

// The Catalog shell: a layout route wrapping every registry page in the
// two-column chrome. The subrail is NAVIGATION, not a filter: each entry is a
// link to the registry's own routed page (which renders in the right pane with
// its real FilterBar, create button, and blades), Overview leads to /catalog,
// and the URL stays canonical (/products, /metrics, ...). Groups, membership,
// order, labels, gates, and count sources all come from the CATALOG_GROUPS
// table in lib/catalog.ts, the same table the Overview cards derive from, so
// the two surfaces can never drift. Counts ride the registries' own shared
// list-query keys (one cache with the pages); a gated entry drops, and with it
// its query; an emptied group drops its header; secret types keep no entry at
// all (the standing ruling), though /secret-types still renders in the pane.

const owner: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "view" }, permissions: ["*:read"], grants: [] };
const meWith = (permissions: string[]): Me => ({ principal: { id: "u-x", kind: "human" }, permissions, grants: [] });

// Rows only need a length: the subrail renders counts, never row fields.
const rows = (n: number) => Array.from({ length: n }, (_, i) => ({ name: `row-${i}` }));

// Every subrail registry's shared cache key, primed with a distinct count,
// keyed exactly as the list pages key them (a shared cache, not a parallel one).
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

function mount(me: Me = owner, opts: { url?: string; skip?: (readonly string[])[] } = {}) {
  window.history.replaceState(null, "", opts.url ?? "/catalog");
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], me);
  for (const [key, n] of PRIMED) {
    if (opts.skip?.some((s) => s.join("/") === key.join("/"))) continue;
    qc.setQueryData([...key], rows(n));
  }
  const result = render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route component={CatalogShell}>
          <Route path="/catalog" component={() => <div>overview lands here</div>} />
          <Route path="/metrics" component={() => <div>metrics page body</div>} />
          <Route path="*" component={() => <div>some page body</div>} />
        </Route>
      </Router>
    </QueryClientProvider>
  ));
  return { ...result, qc };
}

const nav = () => screen.getByRole("navigation", { name: "Catalog sections" });
const hrefs = () => [...nav().querySelectorAll("a")].map((a) => a.getAttribute("href"));

describe("CatalogShell", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders the subrail as links: Overview first, then the groups from the table, in order", () => {
    mount();
    // Overview leads, before any group header.
    const list = nav().querySelector("ul")!;
    expect(list.children[0].textContent).toContain("Overview");
    expect(list.children[0].querySelector("a")!.getAttribute("href")).toBe("/catalog");

    // The group headers, in the table's order (small-caps menu titles).
    const headers = [...nav().querySelectorAll(".menu-title")].map((h) => h.textContent);
    expect(headers).toEqual(CATALOG_GROUPS.map((g) => g.header));

    // Every entry, in column order, as a LINK to its canonical route. The soon
    // slots without a page (the three Templates) are the only non-links.
    expect(hrefs()).toEqual([
      "/catalog",
      "/metrics", "/properties", "/event-types",
      "/command-types", "/rules", "/notifications",
      "/vendors", "/products", "/drivers", "/capabilities", "/component-types",
      "/standards",
      "/location-types",
      "/tags",
    ]);
    // The location-type entry reads "Types" (the Locations header says where it lives).
    expect(nav().querySelector('a[href="/location-types"]')!.textContent).toContain("Types");
  });

  it("renders the routed page in the right pane beside the subrail", () => {
    mount(owner, { url: "/metrics" });
    expect(screen.getByText("metrics page body")).toBeInTheDocument();
    expect(nav()).toBeInTheDocument();
  });

  it("marks the entry matching the route active", () => {
    mount(owner, { url: "/metrics" });
    expect(nav().querySelector('a[href="/metrics"]')!.className).toContain("menu-active");
    expect(nav().querySelector('a[href="/catalog"]')!.className).not.toContain("menu-active");
    expect(nav().querySelector('a[href="/properties"]')!.className).not.toContain("menu-active");
  });

  it("marks Overview active at /catalog", () => {
    mount(owner, { url: "/catalog" });
    expect(nav().querySelector('a[href="/catalog"]')!.className).toContain("menu-active");
    expect(nav().querySelector('a[href="/metrics"]')!.className).not.toContain("menu-active");
  });

  it("shows live counts from the shared query keys, absent until a registry resolves", () => {
    // An unprimed registry's fetch never settles: its count must be absent, not
    // an invented zero, while the primed neighbors show theirs.
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
    mount(owner, { skip: [DRIVERS_KEY] });
    expect(nav().querySelector('a[href="/metrics"]')!.textContent).toContain("12");
    expect(nav().querySelector('a[href="/tags"]')!.textContent).toContain("9");
    expect(nav().querySelector('a[href="/drivers"]')!.textContent).toBe("Drivers");
  });

  it("renders the pageless soon slots (Templates) disabled and non-navigable, the routed ones (Rules, Notifications) as links wearing soon", () => {
    mount();
    const soonButtons = [...nav().querySelectorAll<HTMLButtonElement>("button[disabled]")];
    expect(soonButtons).toHaveLength(3); // one Templates slot per Components / Systems / Locations
    for (const b of soonButtons) {
      expect(b.textContent).toContain("Templates");
      expect(b.textContent).toContain("soon");
    }
    // Rules and Notifications have routed stub pages, so they LINK, wearing the
    // same soon treatment; their counts stay absent (no registry exists).
    for (const path of ["/rules", "/notifications"]) {
      const a = nav().querySelector(`a[href="${path}"]`)!;
      expect(a.textContent).toContain("soon");
      expect(a.textContent).not.toMatch(/\d/);
    }
  });

  it("offers no secret-types entry, per the standing ruling", () => {
    mount();
    expect(nav().querySelector('a[href="/secret-types"]')).toBeNull();
    expect(within(nav()).queryByText("Secret types")).toBeNull();
  });

  it("keeps every entry for a *:read floor viewer: nothing on the subrail is off the read floor", () => {
    mount(viewer);
    expect(hrefs()).toHaveLength(15);
    expect([...nav().querySelectorAll("button[disabled]")]).toHaveLength(3);
  });

  it("drops a gated entry (and never creates its query) and drops an emptied group's header", () => {
    const { qc } = mount(meWith(["metric_type:read"]), { skip: [TAGS_KEY] });
    // Only the ungated Overview, the one readable registry, and the routed soon
    // slots keep their links.
    expect(hrefs()).toEqual(["/catalog", "/metrics", "/rules", "/notifications"]);
    // Metadata's only entry is gated away, so its header drops too; the groups
    // holding soon slots keep theirs.
    const headers = [...nav().querySelectorAll(".menu-title")].map((h) => h.textContent);
    expect(headers).toEqual(["Telemetry", "Actions", "Components", "Systems", "Locations"]);
    // The structural fact: the gated registry's count query was never created,
    // so no fetch can ever have been on its path.
    expect(qc.getQueryCache().find({ queryKey: [...TAGS_KEY] })).toBeUndefined();
  });
});
