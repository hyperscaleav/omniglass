import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor, fireEvent } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Components from "./Components";
import { COMPONENTS_KEY, type Component } from "../lib/components";
import { SYSTEMS_KEY } from "../lib/systems";
import { LOCATIONS_KEY } from "../lib/locations";
import { PRODUCTS_KEY, type Product } from "../lib/products";
import { ME_KEY, type Me } from "../lib/auth";
import { TAGS_KEY, entityTagsKey } from "../lib/tags";
import { uuidFor } from "../lib/testids";
import { hueFor } from "../lib/system_color";

// The Components page on the shared TreeList in the create-as-route model: New routes
// to /components/create (a draft accordion), Save hands off to /components/<name> in
// edit; the detail is read-only in view (no in-body mutation control) and editable via
// the pencil. Data is seeded into the query cache so no server is needed; `>` grants
// every permission.
const me: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const comp: Component = { id: uuidFor("c-1"), name: "mic-2", display_name: "Ceiling Mic 2", product_id: "shure-mxa920", system_count: 0, effective_tags: {} };

// #614: every component is an instance of a product; the create form's
// Product picker offers the real catalog plus the three generics that make
// the classification floor total for anything not yet modeled more
// specifically.
const products: Product[] = [
  { id: uuidFor("prod-shure"), name: "shure-mxa920", display_name: "Shure MXA920", kind: "device", component_type: "ceiling-mic", component_type_id: uuidFor("ct-ceiling-mic"), official: true },
  { id: uuidFor("prod-generic-device"), name: "generic-device", display_name: "Generic Device", kind: "device", component_type: "generic-device", component_type_id: uuidFor("ct-generic-device"), official: true },
  { id: uuidFor("prod-generic-app"), name: "generic-app", display_name: "Generic App", kind: "app", component_type: "generic-app", component_type_id: uuidFor("ct-generic-app"), official: true },
  { id: uuidFor("prod-generic-service"), name: "generic-service", display_name: "Generic Service", kind: "service", component_type: "generic-service", component_type_id: uuidFor("ct-generic-service"), official: true },
];

function mount(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...COMPONENTS_KEY], [comp]);
  qc.setQueryData([...SYSTEMS_KEY], []);
  qc.setQueryData([...LOCATIONS_KEY], []);
  qc.setQueryData([...PRODUCTS_KEY], products);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...TAGS_KEY], []);
  qc.setQueryData([...entityTagsKey("component", "mic-2")], []);
  window.history.pushState({}, "", path);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="/components" component={Components} />
        <Route path="/components/:name" component={Components} />
      </Router>
    </QueryClientProvider>
  ));
}

describe("Components create-as-route", () => {
  afterEach(() => window.history.pushState({}, "", "/"));

  it("wears its system's colour dot on the list row's system column", async () => {
    const sysId = uuidFor("sys-boardroom");
    const withSystem: Component = { ...comp, system: "boardroom", system_id: sysId, system_count: 1 };
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...COMPONENTS_KEY], [withSystem]);
    qc.setQueryData([...SYSTEMS_KEY], [{ id: sysId, name: "boardroom", member_count: 1 }]);
    qc.setQueryData([...LOCATIONS_KEY], []);
    qc.setQueryData([...PRODUCTS_KEY], products);
    qc.setQueryData([...ME_KEY], me);
    qc.setQueryData([...TAGS_KEY], []);
    window.history.pushState({}, "", "/components");
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="/components" component={Components} />
        </Router>
      </QueryClientProvider>
    ));
    await waitFor(() => expect(screen.getByText("Ceiling Mic 2")).toBeTruthy());
    const dot = document.querySelector(".og-system-dot") as HTMLElement;
    expect(dot).toBeTruthy();
    expect(dot.style.getPropertyValue("--sys-h")).toBe(String(hueFor(sysId)));
  });

  // A root component (no component parent) sitting at a location has no
  // ancestor in the PAGE'S OWN tree (the component forest), so the list's
  // client-side pathOf walk finds nothing to show, even though the
  // component plainly sits under that location's rooms (#627 Task 10 is
  // exactly what makes that placement legal). The server's own dash render
  // (renders.dash, #627 Task 15) is what fills that gap; this asserts the
  // row actually shows it in list mode, not just that the data layer
  // carries it (a mocked-fetch test asserting only the request body would
  // pass on a page that fetched the field and never rendered it anywhere).
  it("shows a root component's server-rendered path in list mode, where the local tree walk has none", async () => {
    const placed: Component = { ...comp, renders: { dash: "boi-17c-216b-display-1", bare: "boi17c216bdsp1" } };
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...COMPONENTS_KEY], [placed]);
    qc.setQueryData([...SYSTEMS_KEY], []);
    qc.setQueryData([...LOCATIONS_KEY], []);
    qc.setQueryData([...PRODUCTS_KEY], products);
    qc.setQueryData([...ME_KEY], me);
    qc.setQueryData([...TAGS_KEY], []);
    // Force list (flattened) mode: the tree-local ancestor path (Row.path)
    // renders only in flattened mode, and a root's tree-local path is empty
    // regardless, so this isolates pathRender as the only possible source.
    localStorage.setItem("og-cmp-view", "list");
    window.history.pushState({}, "", "/components");
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="/components" component={Components} />
        </Router>
      </QueryClientProvider>
    ));
    await waitFor(() => expect(screen.getByText("Ceiling Mic 2")).toBeTruthy());
    expect(screen.getByText("boi-17c-216b-display-1")).toBeTruthy();
    localStorage.removeItem("og-cmp-view");
  });

  it("renders the draft-create accordion at /components/create", async () => {
    mount("/components/create");
    await waitFor(() => expect(screen.getByText("New component")).toBeTruthy());
    expect(screen.getByText("Draft")).toBeTruthy();
    expect(screen.getByText("Create component")).toBeTruthy();
    // Identity + Placement fields present; the binding sections are locked.
    expect(screen.getByText("Name")).toBeTruthy();
    expect(screen.getByText("System")).toBeTruthy();
    expect(screen.getByText(/Available once the component is created/)).toBeTruthy();
  });

  it("shows an existing component read-only in view: no tag add control, an Edit affordance", async () => {
    mount("/components/mic-2");
    // The detail resolves and renders the read-only facts.
    await waitFor(() => expect(screen.getByText("Name")).toBeTruthy());
    // No in-body mutation control in view: the TagAdder add row is absent.
    expect(screen.queryByPlaceholderText(/Add a tag/)).toBeNull();
    // The view footer offers Edit (which would flip the accordion to edit mode).
    expect(screen.getByText("Edit")).toBeTruthy();
  });

  it("edit mode exposes an editable name with a check button", async () => {
    mount("/components/mic-2");
    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    // The name becomes an editable input seeded from the row.
    const nameInput = (await screen.findByDisplayValue("mic-2")) as HTMLInputElement;
    expect(nameInput.disabled).toBe(false);
    // An inline check button sits beside it.
    expect(screen.getByLabelText("Check name")).toBeTruthy();
  });

  it("a fresh detail view keeps the name read-only", async () => {
    mount("/components/mic-2");
    await waitFor(() => expect(screen.getByText("Name")).toBeTruthy());
    // No check button until edit begins: the name is a read-only fact.
    expect(screen.queryByLabelText("Check name")).toBeNull();
  });

  // Regression for #336: TreeList created its blade controller but never provided it
  // through context, so a blade BODY calling useBlades (both interface blades do)
  // threw before rendering, and Add interface was dead on every TreeList page. Also
  // pins #332 on this surface: the create form's action sits on the blade's footer
  // rail, and the form itself renders no buttons.
  it("opens the new-interface blade, with its action on the blade rail and none in the form", async () => {
    mount("/components/mic-2");
    await waitFor(() => expect(screen.getByText("Add interface")).toBeTruthy());
    fireEvent.click(screen.getByText("Add interface"));

    // The body rendered at all, which is the regression: no useBlades throw.
    await waitFor(() => expect(screen.getByText(/An API on a component/)).toBeTruthy());

    const blades = document.querySelectorAll("aside[data-blade]");
    const top = blades[blades.length - 1] as HTMLElement;
    const submit = screen.getByText("Create interface").closest("button")!;
    expect(top.querySelector("footer")!.contains(submit)).toBe(true);
    expect(top.querySelector("form")!.querySelectorAll("button").length).toBe(0);
  });
});

// #627 scopes name uniqueness to placement, not the whole estate: two
// components under different parents may now legally share a name. The tree
// builder used to key its construction-time map on the bare name
// (byId.set(c.name, ...)), so the second same-named row silently overwrote
// the first and its children reparented onto the survivor. Keying that map
// on uuid instead (node.id itself stays the name; only the construction key
// moved) is what keeps both rows in the rendered tree.
describe("Components list survives duplicate names across placements (#627)", () => {
  afterEach(() => window.history.pushState({}, "", "/"));

  it("renders both same-named components when they sit under different parents, each keeping its own child", async () => {
    // Each "port-1" has its OWN child (sub-a / sub-b): a name-keyed
    // construction map does not just drop a row, a bare row count could
    // still look right off a double-push artifact (the surviving node
    // object gets pushed into both parents' children arrays). The
    // discriminating symptom the amendment actually describes is the
    // CHILD reparenting onto whichever same-named node won the map: under
    // the old bug, both sub-a and sub-b end up merged onto one surviving
    // "port-1" object and so both appear TWICE (once under each rack, since
    // that one surviving object is what got pushed into both racks'
    // children); under the fix, each sub renders exactly once, under its
    // own parent.
    const rackA: Component = { id: uuidFor("c-rack-a"), name: "rack-a", system_count: 0, effective_tags: {} };
    const rackB: Component = { id: uuidFor("c-rack-b"), name: "rack-b", system_count: 0, effective_tags: {} };
    const portInA: Component = { id: uuidFor("c-port-a"), name: "port-1", parent: "rack-a", parent_id: rackA.id, system_count: 0, effective_tags: {} };
    const portInB: Component = { id: uuidFor("c-port-b"), name: "port-1", parent: "rack-b", parent_id: rackB.id, system_count: 0, effective_tags: {} };
    const subA: Component = { id: uuidFor("c-sub-a"), name: "sub-a", parent: "port-1", parent_id: portInA.id, system_count: 0, effective_tags: {} };
    const subB: Component = { id: uuidFor("c-sub-b"), name: "sub-b", parent: "port-1", parent_id: portInB.id, system_count: 0, effective_tags: {} };

    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...COMPONENTS_KEY], [rackA, rackB, portInA, portInB, subA, subB]);
    qc.setQueryData([...SYSTEMS_KEY], []);
    qc.setQueryData([...LOCATIONS_KEY], []);
    qc.setQueryData([...PRODUCTS_KEY], products);
    qc.setQueryData([...ME_KEY], me);
    qc.setQueryData([...TAGS_KEY], []);
    window.history.pushState({}, "", "/components");
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="/components" component={Components} />
        </Router>
      </QueryClientProvider>
    ));

    await waitFor(() => expect(screen.getAllByText("rack-a").length).toBeGreaterThan(0));
    // Tree mode starts fully collapsed, so expand everything.
    fireEvent.click(screen.getByTitle("Expand all"));
    await waitFor(() => expect(screen.getAllByText("port-1")).toHaveLength(2));
    // Each own child renders exactly once, not twice (merged onto a single
    // surviving "port-1" and pushed out under both racks).
    expect(screen.getAllByText("sub-a")).toHaveLength(1);
    expect(screen.getAllByText("sub-b")).toHaveLength(1);
    // sub-a sits under the SAME "port-1" row as rack-a, sub-b under rack-b's:
    // the tree renders depth-first, so sub-a's row falls strictly between
    // rack-a's and rack-b's, and sub-b's falls after rack-b's.
    const rows = Array.from(document.querySelectorAll("tbody tr"));
    const indexOf = (text: string) => rows.indexOf(screen.getByText(text).closest("tr")!);
    expect(indexOf("sub-a")).toBeGreaterThan(indexOf("rack-a"));
    expect(indexOf("sub-a")).toBeLessThan(indexOf("rack-b"));
    expect(indexOf("sub-b")).toBeGreaterThan(indexOf("rack-b"));
  });

  // A review caught that the earlier fix only moved the tree Map's
  // construction key to uuid; TreeList's own second index (byId, built off
  // node.id) and the row rendering both still keyed on the bare name, so the
  // collapse this task set out to remove simply moved one layer down: opening
  // either duplicate's row rendered whichever one the Map happened to keep,
  // silent wrong data (the wrong uuid, the wrong placement), not just an
  // ambiguous URL. This test is the one that discriminates: it clicks
  // rack-a's port-1 row specifically and asserts the blade that opens shows
  // rack-a as the Parent, never rack-b, which only holds once node.id is the
  // true uuid end to end (the tree index, the blade lookup, and the detail
  // body's own re-resolve all key on the same id).
  it("opens the blade for the duplicate that was actually clicked, not whichever one a name-keyed index kept (#627)", async () => {
    const rackA: Component = { id: uuidFor("c-rack-a"), name: "rack-a", system_count: 0, effective_tags: {} };
    const rackB: Component = { id: uuidFor("c-rack-b"), name: "rack-b", system_count: 0, effective_tags: {} };
    const portInA: Component = { id: uuidFor("c-port-a"), name: "port-1", parent: "rack-a", parent_id: rackA.id, system_count: 0, effective_tags: {} };
    const portInB: Component = { id: uuidFor("c-port-b"), name: "port-1", parent: "rack-b", parent_id: rackB.id, system_count: 0, effective_tags: {} };

    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...COMPONENTS_KEY], [rackA, rackB, portInA, portInB]);
    qc.setQueryData([...SYSTEMS_KEY], []);
    qc.setQueryData([...LOCATIONS_KEY], []);
    qc.setQueryData([...PRODUCTS_KEY], products);
    qc.setQueryData([...ME_KEY], me);
    qc.setQueryData([...TAGS_KEY], []);
    window.history.pushState({}, "", "/components");
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="/components" component={Components} />
        </Router>
      </QueryClientProvider>
    ));

    await waitFor(() => expect(screen.getAllByText("rack-a").length).toBeGreaterThan(0));
    fireEvent.click(screen.getByTitle("Expand all"));
    await waitFor(() => expect(screen.getAllByText("port-1")).toHaveLength(2));

    // rack-a's port-1 row is the one strictly between rack-a's own row and
    // rack-b's, per the depth-first tree order the test above already pins.
    const rows = Array.from(document.querySelectorAll("tbody tr"));
    const rackARowIndex = rows.indexOf(screen.getByText("rack-a").closest("tr")!);
    const rackBRowIndex = rows.indexOf(screen.getByText("rack-b").closest("tr")!);
    const portRows = screen.getAllByText("port-1").map((el) => el.closest("tr")!);
    const portInARow = portRows.find((r) => {
      const idx = rows.indexOf(r);
      return idx > rackARowIndex && idx < rackBRowIndex;
    });
    expect(portInARow).toBeTruthy();
    fireEvent.click(portInARow!);

    const blade = await waitFor(() => {
      const el = document.querySelector("aside[data-blade]") as HTMLElement | null;
      if (!el || !el.textContent?.includes("Parent")) throw new Error("blade not open yet");
      return el;
    });
    await waitFor(() => expect(blade.textContent).toContain("rack-a"));
    expect(blade.textContent).not.toContain("rack-b");
  });
});

// #614: component.product_id is NOT NULL. A component cannot exist without a
// product, so the create form makes Product a required field and offers the
// three generics (generic-device/app/service) as the fallback choice for
// anything not yet modeled as a real SKU.
describe("Components create requires a product (#614)", () => {
  afterEach(() => {
    window.history.pushState({}, "", "/");
    vi.restoreAllMocks();
  });

  it("renders a Product field and blocks Create component until one is chosen", async () => {
    mount("/components/create");
    await waitFor(() => expect(screen.getByText("New component")).toBeTruthy());
    fireEvent.input(screen.getByPlaceholderText("Ceiling Mic 2"), { target: { value: "Spare Panel" } });
    const submit = screen.getByText("Create component").closest("button") as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
    const productSelect = screen.getByLabelText("Product") as HTMLSelectElement;
    fireEvent.change(productSelect, { target: { value: "shure-mxa920" } });
    expect(submit.disabled).toBe(false);
  });

  it("offers the generics as fallback choices in the Product picker", async () => {
    mount("/components/create");
    await waitFor(() => expect(screen.getByText("New component")).toBeTruthy());
    const productSelect = screen.getByLabelText("Product") as HTMLSelectElement;
    const labels = Array.from(productSelect.options).map((o) => o.textContent);
    expect(labels).toContain("Shure MXA920");
    expect(labels).toContain("Generic Device");
    expect(labels).toContain("Generic App");
    expect(labels).toContain("Generic Service");
  });

  it("sends the chosen product on create, generic or real", async () => {
    let sent: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST" && req.url.endsWith("/components")) {
        sent = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({ ...comp, name: "spare-panel", product: "generic-device" }), { status: 201, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ components: [] }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount("/components/create");
    await waitFor(() => expect(screen.getByText("New component")).toBeTruthy());
    fireEvent.input(screen.getByPlaceholderText("Ceiling Mic 2"), { target: { value: "Spare Panel" } });
    fireEvent.change(screen.getByLabelText("Product"), { target: { value: "generic-device" } });
    fireEvent.click(screen.getByText("Create component"));
    await waitFor(() => expect(sent).toBeTruthy());
    expect((sent as { product: string }).product).toBe("generic-device");
  });
});
