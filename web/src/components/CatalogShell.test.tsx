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
import { STANDARDS_KEY } from "../lib/standards";
import { LOCATION_TYPES_KEY } from "../lib/location_types";
import { METRICS_KEY } from "../lib/metric_types";
import { PROPERTIES_KEY } from "../lib/properties";
import { EVENT_TYPES_KEY } from "../lib/event_types";
import { COMMAND_TYPES_KEY } from "../lib/command_types";
import { NOTIFICATION_TYPES_KEY } from "../lib/notification_types";
import { EVENT_RULES_KEY } from "../lib/event_rules";
import { CALC_RULES_KEY } from "../lib/calc_rules";
import { severityLevelsKey } from "../lib/severity";
import { TAGS_KEY } from "../lib/tags";
import { TEMPLATES_KEY } from "../lib/templates";

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
  [STANDARDS_KEY, 4],
  [LOCATION_TYPES_KEY, 5],
  [METRICS_KEY, 12],
  [PROPERTIES_KEY, 7],
  [EVENT_TYPES_KEY, 15],
  [COMMAND_TYPES_KEY, 6],
  [NOTIFICATION_TYPES_KEY, 2],
  [EVENT_RULES_KEY, 3],
  [CALC_RULES_KEY, 2],
  [severityLevelsKey, 5],
  [TAGS_KEY, 9],
];

// The one template catalog list the three Templates slots share, each counting
// its own kinds of it: 5 component, 2 standard + 2 system, 1 place.
const TEMPLATE_ROWS = [
  ...Array.from({ length: 5 }, (_, i) => ({ name: `c-${i}`, kind: "component_template" })),
  ...Array.from({ length: 2 }, (_, i) => ({ name: `st-${i}`, kind: "standard_template" })),
  ...Array.from({ length: 2 }, (_, i) => ({ name: `sy-${i}`, kind: "system_template" })),
  { name: "p-0", kind: "place_template" },
];

function mount(me: Me = owner, opts: { url?: string; skip?: (readonly string[])[] } = {}) {
  window.history.replaceState(null, "", opts.url ?? "/catalog");
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], me);
  for (const [key, n] of PRIMED) {
    if (opts.skip?.some((s) => s.join("/") === key.join("/"))) continue;
    qc.setQueryData([...key], rows(n));
  }
  qc.setQueryData([...TEMPLATES_KEY], TEMPLATE_ROWS);
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

    // Every entry, in column order, as a LINK to its canonical route. The three
    // Templates slots open the one Templates page filtered to their kinds (#1329).
    expect(hrefs()).toEqual([
      "/catalog",
      "/metrics", "/properties", "/event-types",
      "/command-types", "/rules", "/calc-rules", "/severity-levels", "/notifications",
      "/vendors", "/products", "/drivers", "/component-types", "/templates?kind=component_template",
      "/standards", "/system-types", "/templates?kind=standard_template,system_template",
      "/location-types", "/templates?kind=place_template",
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

  it("renders the Templates slots live, each counting its own kinds of the one catalog list, and Notifications as a live link (#618)", () => {
    mount();
    expect([...nav().querySelectorAll("button[disabled]")]).toHaveLength(0);
    const slot = (q: string) => nav().querySelector(`a[href="/templates?kind=${q}"]`)!;
    expect(slot("component_template").textContent).toBe("Templates5");
    expect(slot("standard_template,system_template").textContent).toBe("Templates4");
    expect(slot("place_template").textContent).toBe("Templates1");
    // Notifications went live (#618): a plain link with its count.
    const a = nav().querySelector(`a[href="/notifications"]`)!;
    expect(a.textContent).toContain("2");
    expect(a.textContent).not.toContain("soon");
    // Rules went live (#921): a plain link with its count, no soon treatment.
    const rules = nav().querySelector(`a[href="/rules"]`)!;
    expect(rules.textContent).toContain("3");
    expect(rules.textContent).not.toContain("soon");
  });

  it("lights only the Templates slot whose kinds are showing", () => {
    mount(owner, { url: "/templates?kind=place_template" });
    const active = [...nav().querySelectorAll("a.menu-active")].map((a) => a.getAttribute("href"));
    expect(active).toEqual(["/templates?kind=place_template"]);
    const current = [...nav().querySelectorAll('a[aria-current="page"]')].map((a) => a.getAttribute("href"));
    expect(current).toEqual(["/templates?kind=place_template"]);
  });

  it("lights no Templates slot on the unfiltered catalog", () => {
    mount(owner, { url: "/templates" });
    expect(nav().querySelectorAll('a[href^="/templates"].menu-active, a[href^="/templates"][aria-current]')).toHaveLength(0);
  });

  it("offers no secret-types entry, per the standing ruling", () => {
    mount();
    expect(nav().querySelector('a[href="/secret-types"]')).toBeNull();
    expect(within(nav()).queryByText("Secret types")).toBeNull();
  });

  it("keeps every entry for a *:read floor viewer: nothing on the subrail is off the read floor", () => {
    mount(viewer);
    expect(hrefs()).toHaveLength(20);
    expect([...nav().querySelectorAll("button[disabled]")]).toHaveLength(0);
  });

  it("drops a gated entry (and never creates its query) and drops an emptied group's header", () => {
    const { qc } = mount(meWith(["metric_type:read"]), { skip: [TAGS_KEY] });
    // Only the ungated Overview and the one readable registry keep their links
    // (Rules, Notifications and the Templates slots are gated on their reads).
    expect(hrefs()).toEqual(["/catalog", "/metrics"]);
    // Every other group's entries are gated away, so their headers drop too.
    const headers = [...nav().querySelectorAll(".menu-title")].map((h) => h.textContent);
    expect(headers).toEqual(["Telemetry"]);
    // The structural fact: the gated registry's count query was never created,
    // so no fetch can ever have been on its path.
    expect(qc.getQueryCache().find({ queryKey: [...TAGS_KEY] })).toBeUndefined();
  });
});
