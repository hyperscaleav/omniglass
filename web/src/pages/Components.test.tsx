import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor, fireEvent, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Components from "./Components";
import { COMPONENTS_KEY, type Component } from "../lib/components";
import { SYSTEMS_KEY, type System } from "../lib/systems";
import { LOCATIONS_KEY, type Location } from "../lib/locations";
import { PRODUCTS_KEY, type Product } from "../lib/products";
import { COMPONENT_TYPES_KEY, type ComponentType } from "../lib/component_types";
import { ME_KEY, type Me } from "../lib/auth";
import { TAGS_KEY, entityTagsKey } from "../lib/tags";
import { uuidFor } from "../lib/testids";
import { hueFor } from "../lib/system_color";
import { INTERFACES_KEY, type Interface } from "../lib/interfaces";
import { REACHABILITY_KEY } from "../lib/reachability";
import { NAME_MIN_W } from "../components/TreeList";

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

// The device-class registry the create form resolves a generated name's stem
// through. Three tiers on purpose: ceiling-mic sets no stem of its own, so a
// preview that read the product's own type and stopped would answer nothing
// where the server's inheriting walk answers "mic".
const componentTypes: ComponentType[] = [
  { id: uuidFor("ct-device"), name: "device", display_name: "Device", official: true, forked: false, stem: "device", default_tags: [] },
  { id: uuidFor("ct-mic"), name: "mic", display_name: "Microphone", official: true, forked: false, parent: "device", stem: "mic", default_tags: [] },
  { id: uuidFor("ct-ceiling-mic"), name: "ceiling-mic", display_name: "Ceiling Microphone", official: true, forked: false, parent: "mic", default_tags: [] },
  { id: uuidFor("ct-generic-device"), name: "generic-device", display_name: "Generic Device", official: true, forked: false, stem: "device", default_tags: [] },
  { id: uuidFor("ct-generic-app"), name: "generic-app", display_name: "Generic App", official: true, forked: false, stem: "app", default_tags: [] },
  { id: uuidFor("ct-generic-service"), name: "generic-service", display_name: "Generic Service", official: true, forked: false, stem: "service", default_tags: [] },
];

// One room, so the create form's placement has both a parent candidate and a
// location candidate: the bucket precedence (a parent wins) cannot be witnessed
// with only one of the two set, and a page passing the two the wrong way round
// is invisible to a unit test of the rule.
const room: Location = { id: uuidFor("l-room"), name: "boardroom", display_name: "Boardroom", location_type: "room", effective_tags: {} };

// Two systems, told apart by their ACTIONS rather than by anything an operator
// reads (#707 review). actions is the server's per-row answer computed from the
// same per-action scope the gateway enforces, so "which systems may this caller
// bind" is data the console filters, never a scope it resolves. board carries
// update; annex is readable and not writable, which is what a location-scoped
// deploy grant beside the viewer floor produces for every system in the estate.
const systems: System[] = [
  { id: uuidFor("s-board"), name: "board", display_name: "Boardroom AV", member_count: 0, actions: ["create", "update", "delete"] },
  { id: uuidFor("s-annex"), name: "annex", display_name: "Annex AV", member_count: 0, actions: [] },
];

function mount(path: string, who: Me = me, sys: System[] = systems) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...COMPONENTS_KEY], [comp]);
  qc.setQueryData([...SYSTEMS_KEY], sys);
  qc.setQueryData([...LOCATIONS_KEY], [room]);
  qc.setQueryData([...PRODUCTS_KEY], products);
  qc.setQueryData([...COMPONENT_TYPES_KEY], componentTypes);
  qc.setQueryData([...ME_KEY], who);
  qc.setQueryData([...TAGS_KEY], []);
  qc.setQueryData([...entityTagsKey("component", "mic-2")], []);
  window.history.pushState({}, "", path);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="/components" component={Components} />
        <Route path="/components/:id" component={Components} />
      </Router>
    </QueryClientProvider>
  ));
}

// Both identity fields open LOCKED on the platform's answer (#699), so a test
// that means to type into one takes the pen first, exactly as an operator does.
// The lock is a square icon button inside each field's join and carries no text
// (#657), so each is addressed by its accessible name, which is also what makes
// the two tell apart for a screen reader.
function unlockName() {
  fireEvent.click(screen.getByRole("button", { name: "Override the name" }));
}
function unlockLabel() {
  fireEvent.click(screen.getByRole("button", { name: "Override the display name" }));
}

// The server's draft answer, which is where the locked NAME comes from since
// #702. The console used to resolve the stem itself and write the token "n"
// where the ordinal went; it shows what this returns, ordinal and all, so every
// create-form test has to serve the route. The names below are fixtures, not a
// second implementation of the mint: that the gateway mints "mic-1" is proven
// against a real database in internal/storage, and what this file proves is
// that the form shows what the gateway said and posts the number back.
const DRAFTED: Record<string, string> = {
  "shure-mxa920": "mic-1",
  "generic-app": "app-1",
  "generic-device": "device-1",
};

function draftJSON(body: { product?: string; name?: string }, label = "", rule = "") {
  // An operator-typed name comes back as itself with NO ordinal, exactly as the
  // route answers it: that row carries none, so there is no precondition to
  // post beside it.
  const drafted = body.name
    ? { name: body.name, label, rule }
    : { name: DRAFTED[body.product ?? ""] ?? "thing-1", ordinal: 1, label, rule };
  return new Response(JSON.stringify(drafted), { status: 200, headers: { "Content-Type": "application/json" } });
}

// stubFetch serves the draft route and hands everything else to rest, which
// throws by default: a create-form test that reaches an unexpected endpoint
// should say so rather than hang.
function stubFetch(rest?: (req: Request) => Promise<Response> | Response) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const req = input as Request;
    if (req.method === "POST" && req.url.includes(":renderLabel")) {
      return draftJSON(JSON.parse(await req.clone().text()));
    }
    if (rest) return rest(req);
    throw new Error(`unexpected fetch in this test: ${req.method} ${req.url}`);
  });
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
    qc.setQueryData([...COMPONENT_TYPES_KEY], componentTypes);
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

  // #627 Task 15c: the cross-entity drill from Systems.tsx's own "Components"
  // button now emits ?system=<uuid>, and this facet must match it: a name
  // would collide (or miss entirely) once two systems can share one under
  // different placements (#627 Task 10). Asserts the query-string drill-in
  // yields the same row set a manual system chip would.
  it("filters to a system's components from a ?system=<uuid> deep link, matching by id not name", async () => {
    const sysId = uuidFor("sys-boardroom");
    const otherSysId = uuidFor("sys-annex");
    const inSystem: Component = { ...comp, id: uuidFor("c-in"), name: "mic-in", display_name: "In-room Mic", system: "boardroom", system_id: sysId, system_count: 1 };
    const outOfSystem: Component = { ...comp, id: uuidFor("c-out"), name: "mic-out", display_name: "Annex Mic", system: "annex-room", system_id: otherSysId, system_count: 1 };
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...COMPONENTS_KEY], [inSystem, outOfSystem]);
    qc.setQueryData([...SYSTEMS_KEY], [
      { id: sysId, name: "boardroom", member_count: 1 },
      { id: otherSysId, name: "annex-room", member_count: 1 },
    ]);
    qc.setQueryData([...LOCATIONS_KEY], []);
    qc.setQueryData([...PRODUCTS_KEY], products);
    qc.setQueryData([...COMPONENT_TYPES_KEY], componentTypes);
    qc.setQueryData([...ME_KEY], me);
    qc.setQueryData([...TAGS_KEY], []);
    window.history.pushState({}, "", `/components?system=${sysId}`);
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="/components" component={Components} />
        </Router>
      </QueryClientProvider>
    ));
    await waitFor(() => expect(screen.getByText("In-room Mic")).toBeTruthy());
    expect(screen.queryByText("Annex Mic")).toBeNull();
    // Review finding 4 (task-15-review.md #4): the committed chip must show
    // the system's own readable label, not the raw uuid the query string
    // and the facet's own value now carry. Scoped to the chip's own value
    // button (font-data font-medium): "boardroom" also legitimately
    // appears in the row's own System column, so an unscoped query would
    // pass whether or not the chip itself carries the label.
    const chipValue = document.querySelector(".font-data.font-medium");
    expect(chipValue?.textContent).toBe("boardroom");
    expect(screen.queryByText(sysId)).toBeNull();
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
    qc.setQueryData([...COMPONENT_TYPES_KEY], componentTypes);
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

  // #627 Task 15d: the name field is optional now (the generator mints one
  // from the product's component_type when it is left blank), and the create
  // button must not require it the way it required a product.
  it("does not require a name to submit the create form, only the product", async () => {
    stubFetch();
    mount("/components/create");
    await waitFor(() => expect(screen.getByText("Create component")).toBeTruthy());
    const submit = screen.getByText("Create component").closest("button") as HTMLButtonElement;
    expect(submit.disabled).toBe(true); // no product chosen yet
    const productSelect = (await screen.findByLabelText("Product")) as HTMLSelectElement;
    fireEvent.change(productSelect, { target: { value: "shure-mxa920" } });
    // Enabled once the platform has ANSWERED, which is a round trip since #702
    // rather than a synchronous read of a registry the picker had loaded. The
    // wait is the behaviour change: submitting before the answer lands would
    // post no precondition, so the gate holds until there is one.
    await waitFor(() => expect(submit.disabled).toBe(false)); // name still blank, and that is fine
  });

  it("omits name from the create POST body when the field is left blank", async () => {
    mount("/components/create");
    await waitFor(() => expect(screen.getByText("Create component")).toBeTruthy());
    const productSelect = (await screen.findByLabelText("Product")) as HTMLSelectElement;
    fireEvent.change(productSelect, { target: { value: "shure-mxa920" } });
    // The display name is filled in (an operator label, independent of the
    // address); the name field is left untouched. createIdentity's old
    // derive-from-display coupling would have filled it anyway (#627 Task
    // 15d retires that path for this form specifically).
    const displayInput = screen.getByPlaceholderText("Ceiling Mic 2") as HTMLInputElement;
    unlockLabel();
    fireEvent.input(displayInput, { target: { value: "Ceiling Mic 9" } });
    // The name field is LOCKED (#699), so what it shows is the platform's
    // answer and its pen holds nothing: that is what makes the body below omit
    // the field rather than post the shape as a literal name.
    const nameInput = screen.getByPlaceholderText("mic-2 (optional)") as HTMLInputElement;
    await waitFor(() => expect(nameInput.value).toBe("mic-1"));
    expect(nameInput.readOnly).toBe(true);
    let captured: unknown;
    stubFetch(async (req) => {
      if (req.method === "POST" && req.url.endsWith("/components")) {
        captured = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({ ...comp, id: uuidFor("c-generated"), name: "mic-1", name_generated: true }), { status: 201, headers: { "Content-Type": "application/json" } });
      }
      if (req.method === "GET") {
        return new Response(JSON.stringify({ components: [comp] }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      throw new Error(`unexpected fetch in this test: ${req.method} ${req.url}`);
    });
    fireEvent.click(screen.getByText("Create component"));
    await waitFor(() => expect(captured).toBeTruthy(), { timeout: 3000 });
    const body = captured as Record<string, unknown>;
    expect("name" in body).toBe(false);
    // What IS posted is the NAME the locked field was showing (#702, and its
    // review): a precondition, never the name field, so the pen stays with the
    // platform. The name and not the ordinal, because the name is what the
    // operator was shown and it carries the stem as well as the number.
    expect(body.expected_name).toBe("mic-1");
    expect(body.display_name).toBe("Ceiling Mic 9");
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

  // #644: names are unique within their PLACEMENT since #627, so two different
  // rooms can legally both be called "415a". Addressing the precheck's location
  // by name therefore cannot say which one is meant: the request errors and the
  // catch swallows it into silence, so the operator loses the live "this name is
  // taken" hint for exactly the estates where duplicate names are normal. The
  // row already carries the uuid, and :checkName dual-accepts one (ADR-0062).
  it("addresses the name precheck's placement by uuid, not by a placement-scoped name", async () => {
    const locID = uuidFor("loc-415a");
    const placed: Component = { ...comp, location: "415a", location_id: locID };
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...COMPONENTS_KEY], [placed]);
    qc.setQueryData([...SYSTEMS_KEY], []);
    qc.setQueryData([...LOCATIONS_KEY], [{ id: locID, name: "415a" }]);
    qc.setQueryData([...PRODUCTS_KEY], products);
    qc.setQueryData([...COMPONENT_TYPES_KEY], componentTypes);
    qc.setQueryData([...ME_KEY], me);
    qc.setQueryData([...TAGS_KEY], []);
    qc.setQueryData([...entityTagsKey("component", "mic-2")], []);
    window.history.pushState({}, "", "/components/mic-2");
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="/components" component={Components} />
          <Route path="/components/:id" component={Components} />
        </Router>
      </QueryClientProvider>
    ));

    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    const nameInput = (await screen.findByDisplayValue("mic-2")) as HTMLInputElement;

    let captured: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const { method, url } = req;
      if (method === "POST" && url.includes("checkName")) {
        captured = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({ available: true }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({}), { status: 200, headers: { "Content-Type": "application/json" } });
    });

    // The check button is disabled while the name still equals the stored one,
    // so a precheck only ever runs against a name the operator has changed.
    fireEvent.input(nameInput, { target: { value: "mic-3" } });
    fireEvent.click(screen.getByLabelText("Check name"));
    await waitFor(() => expect(captured).toBeTruthy());
    const body = captured as Record<string, unknown>;
    expect(body.location).toBe(locID);
    expect(body.location).not.toBe("415a");
  });

  it("a fresh detail view keeps the name read-only", async () => {
    mount("/components/mic-2");
    await waitFor(() => expect(screen.getByText("Name")).toBeTruthy());
    // No check button until edit begins: the name is a read-only fact.
    expect(screen.queryByLabelText("Check name")).toBeNull();
  });

  // #627 Task 15d: the tracking chip is the only place an operator learns a
  // name is platform-owned (renaming clears the flag for good, with no
  // other visible cue beforehand).
  it("shows a Generated tracking chip on a platform-picked name, not on an operator-typed one", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    const generated: Component = { ...comp, name_generated: true };
    qc.setQueryData([...COMPONENTS_KEY], [generated]);
    qc.setQueryData([...SYSTEMS_KEY], []);
    qc.setQueryData([...LOCATIONS_KEY], []);
    qc.setQueryData([...PRODUCTS_KEY], products);
    qc.setQueryData([...COMPONENT_TYPES_KEY], componentTypes);
    qc.setQueryData([...ME_KEY], me);
    qc.setQueryData([...TAGS_KEY], []);
    qc.setQueryData([...entityTagsKey("component", "mic-2")], []);
    window.history.pushState({}, "", "/components/mic-2");
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="/components" component={Components} />
          <Route path="/components/:id" component={Components} />
        </Router>
      </QueryClientProvider>
    ));
    expect(await screen.findByText("Generated")).toBeTruthy();
  });

  it("shows no tracking chip on an operator-typed name", async () => {
    mount("/components/mic-2"); // comp.name_generated is unset (falsy)
    await waitFor(() => expect(screen.getByText("Name")).toBeTruthy());
    expect(screen.queryByText("Generated")).toBeNull();
  });

  // Review minor (task-15-review.md, Minors): :resetName is gated by
  // component:rename at the API, strictly narrower than the component:update
  // that opens edit mode at all, so the button must not render for an
  // operator who holds one but not the other.
  it("hides the reset affordance from an operator who can edit but not rename", async () => {
    const limitedMe: Me = {
      principal: { id: "u-limited", kind: "human" },
      human: { username: "limited" },
      permissions: ["component:update", "component:read"],
      grants: [],
    };
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...COMPONENTS_KEY], [comp]);
    qc.setQueryData([...SYSTEMS_KEY], []);
    qc.setQueryData([...LOCATIONS_KEY], []);
    qc.setQueryData([...PRODUCTS_KEY], products);
    qc.setQueryData([...COMPONENT_TYPES_KEY], componentTypes);
    qc.setQueryData([...ME_KEY], limitedMe);
    qc.setQueryData([...TAGS_KEY], []);
    qc.setQueryData([...entityTagsKey("component", "mic-2")], []);
    window.history.pushState({}, "", "/components/mic-2");
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="/components" component={Components} />
          <Route path="/components/:id" component={Components} />
        </Router>
      </QueryClientProvider>
    ));
    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    await screen.findByDisplayValue("mic-2");
    expect(screen.getByLabelText("Check name")).toBeTruthy();
    expect(screen.queryByLabelText("Reset to generated name")).toBeNull();
  });

  it("the reset affordance calls :resetName and updates the name field from the response", async () => {
    mount("/components/mic-2");
    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    await screen.findByDisplayValue("mic-2");
    let capturedMethod: string | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const { method, url } = req;
      if (method === "POST" && url.includes(`/components/${comp.id}:resetName`)) {
        capturedMethod = method;
        return new Response(JSON.stringify({ ...comp, name: "ceiling-mic-1", name_generated: true }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (method === "GET") {
        return new Response(JSON.stringify({ components: [comp] }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      throw new Error(`unexpected fetch in this test: ${method} ${url}`);
    });
    fireEvent.click(screen.getByLabelText("Reset to generated name"));
    await waitFor(() => expect(capturedMethod).toBe("POST"));
    // The local draft reflects the server's own regenerated name immediately,
    // not just after a background refetch lands.
    expect(await screen.findByDisplayValue("ceiling-mic-1")).toBeTruthy();
  });

  // Review finding 1 (task-15-review.md #3): two components sharing a name in
  // different rooms is #627's own default outcome (the first display in every
  // room is named "display-1"), and the URL already carries the uuid (#627
  // Task 15c), but every write on the detail page still addressed the server
  // by n().raw.name. Against two same-named rows that is genuinely ambiguous
  // (ErrAmbiguousName, mapped to a 409), so Save, Delete, and Reset all
  // refused on exactly the row this task's own disambiguation chooser routes
  // an operator to. This test names the two components identically and
  // asserts every write (PATCH, :rename, :resetName, DELETE) targets the
  // path-addressed component's uuid, never the shared name; it fails if any
  // call site regresses to n().raw.name.
  it("addresses every write on the detail page by uuid, not by name, so a duplicate-named component stays editable", async () => {
    const twinA: Component = { ...comp, id: uuidFor("c-twin-a"), name: "twin", location: "room-a", location_id: uuidFor("loc-room-a") };
    const twinB: Component = { ...comp, id: uuidFor("c-twin-b"), name: "twin", location: "room-b", location_id: uuidFor("loc-room-b") };
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...COMPONENTS_KEY], [twinA, twinB]);
    qc.setQueryData([...SYSTEMS_KEY], []);
    qc.setQueryData([...LOCATIONS_KEY], []);
    qc.setQueryData([...PRODUCTS_KEY], products);
    qc.setQueryData([...COMPONENT_TYPES_KEY], componentTypes);
    qc.setQueryData([...ME_KEY], me);
    qc.setQueryData([...TAGS_KEY], []);
    qc.setQueryData([...entityTagsKey("component", "twin")], []);
    // The route already carries twinA's uuid (#627 Task 15c); the ambiguity
    // is only in what the detail page's own writes address by, not in which
    // row opens.
    window.history.pushState({}, "", `/components/${twinA.id}`);
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="/components" component={Components} />
          <Route path="/components/:id" component={Components} />
        </Router>
      </QueryClientProvider>
    ));
    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    const nameInput = (await screen.findByDisplayValue("twin")) as HTMLInputElement;
    fireEvent.input(nameInput, { target: { value: "twin-renamed" } });
    const seen: string[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const { method, url } = req;
      if (method === "PATCH" && url.includes(`/components/${twinA.id}`)) {
        seen.push("patch");
        return new Response(JSON.stringify(twinA), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (method === "POST" && url.includes(`/components/${twinA.id}:rename`)) {
        seen.push("rename");
        return new Response(JSON.stringify({ ...twinA, name: "twin-renamed" }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (method === "GET") {
        return new Response(JSON.stringify({ components: [twinA, twinB] }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      // Anything addressed by the bare (ambiguous) name "twin" is exactly
      // the bug: the server would refuse it with ErrAmbiguousName (409).
      throw new Error(`unexpected fetch in this test: ${method} ${url}`);
    });
    fireEvent.click(screen.getByText("Save changes"));
    // Wait for the save to fully resolve (edit mode exits), not just for the
    // fetch calls to have fired: the accordion's own save() still has an
    // awaited invalidateQueries after the rename resolves, so checking seen
    // alone races the view/edit mode switch the next step depends on.
    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    expect(seen).toContain("patch");
    expect(seen).toContain("rename");

    // Reset: its own immediate act, also uuid-addressed.
    fireEvent.click(screen.getByText("Edit"));
    await screen.findByLabelText("Reset to generated name");
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const { method, url } = req;
      if (method === "POST" && url.includes(`/components/${twinA.id}:resetName`)) {
        seen.push("reset");
        return new Response(JSON.stringify({ ...twinA, name: "twin-1", name_generated: true }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (method === "GET") {
        return new Response(JSON.stringify({ components: [twinA, twinB] }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      throw new Error(`unexpected fetch in this test: ${method} ${url}`);
    });
    fireEvent.click(screen.getByLabelText("Reset to generated name"));
    await waitFor(() => expect(seen).toContain("reset"));
    // Reset does not itself leave edit mode (it is not a Save); Cancel does,
    // uncovering the view-mode footer Delete lives on.
    fireEvent.click(screen.getByText("Cancel"));

    // Delete: same requirement.
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const { method, url } = req;
      if (method === "DELETE" && url.includes(`/components/${twinA.id}`)) {
        seen.push("delete");
        return new Response(null, { status: 204 });
      }
      if (method === "GET") {
        return new Response(JSON.stringify({ components: [twinB] }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      throw new Error(`unexpected fetch in this test: ${method} ${url}`);
    });
    vi.spyOn(window, "confirm").mockReturnValue(true);
    fireEvent.click(screen.getByText("Delete"));
    await waitFor(() => expect(seen).toContain("delete"));
  });

  // #627 Task 15c: routes take :id now. A name-shaped deep link (an old
  // bookmark, or a cross-entity drill site with no id in hand) resolves
  // through TreeList's byAddr fallback and corrects the address bar to the
  // uuid (replace); a rename afterward must leave that URL alone, since the
  // id it addresses never changes.
  it("redirects a name-shaped deep link to the resolved uuid, and a rename leaves the route where it is", async () => {
    mount("/components/mic-2");
    await waitFor(() => expect(window.location.pathname).toBe(`/components/${comp.id}`));
    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    const nameInput = (await screen.findByDisplayValue("mic-2")) as HTMLInputElement;
    fireEvent.input(nameInput, { target: { value: "mic-3" } });
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const { method, url } = req;
      if (method === "PATCH" && url.includes(`/components/${comp.id}`)) {
        return new Response(JSON.stringify(comp), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (method === "POST" && url.includes(`/components/${comp.id}:rename`)) {
        return new Response(JSON.stringify({ ...comp, name: "mic-3" }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (method === "GET") {
        return new Response(JSON.stringify({ components: [{ ...comp, name: "mic-3" }] }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      throw new Error(`unexpected fetch in this test: ${method} ${url}`);
    });
    fireEvent.click(screen.getByText("Save changes"));
    await waitFor(() => expect(screen.queryByLabelText("Check name")).toBeNull());
    expect(window.location.pathname).toBe(`/components/${comp.id}`);
  });

  // Regression: the app's real router (web/src/index.tsx) mounts <Router
  // base="/web">, which `mount()` above deliberately does not reproduce, so
  // the base-less assertion two tests up cannot see a base-prefixed
  // redirect target double the base. useLocation().pathname under a
  // base-carrying router is the FULL path (base included), and passing that
  // straight to navigate() with the router's default resolve semantics
  // re-prepends the base, landing on "/web/web/components/<id>" and a 404.
  // Caught live against `make dev` (a real browser, a real "/web" base),
  // invisible to every other test in this file.
  it("redirects a name-shaped deep link to the resolved uuid under the app's real /web base, without doubling it", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...COMPONENTS_KEY], [comp]);
    qc.setQueryData([...SYSTEMS_KEY], []);
    qc.setQueryData([...LOCATIONS_KEY], []);
    qc.setQueryData([...PRODUCTS_KEY], products);
    qc.setQueryData([...COMPONENT_TYPES_KEY], componentTypes);
    qc.setQueryData([...ME_KEY], me);
    qc.setQueryData([...TAGS_KEY], []);
    qc.setQueryData([...entityTagsKey("component", "mic-2")], []);
    window.history.pushState({}, "", "/web/components/mic-2");
    render(() => (
      <QueryClientProvider client={qc}>
        <Router base="/web">
          <Route path="/components" component={Components} />
          <Route path="/components/:id" component={Components} />
        </Router>
      </QueryClientProvider>
    ));
    await waitFor(() => expect(window.location.pathname).toBe(`/web/components/${comp.id}`));
    expect(window.location.pathname).not.toContain("/web/web/");
    expect(await screen.findByText("Edit")).toBeTruthy();
  });

  it("renders an explicit not-found state for an address that matches no component, not the old silent list fallback", async () => {
    mount("/components/no-such-widget");
    expect(await screen.findByText(/No such component/)).toBeTruthy();
    expect(screen.getByText(/old address/)).toBeTruthy();
    expect(screen.queryByText("Ceiling Mic 2")).toBeNull();
  });

  // Same name, two different placements (#627 Task 10 legalizes this):
  // resolving by name is genuinely ambiguous, so the route renders a
  // disambiguation list rather than guessing which row the operator meant.
  it("renders a disambiguation list when a name-shaped address matches more than one component", async () => {
    const twin: Component = { ...comp, id: uuidFor("c-2"), parent_id: undefined };
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...COMPONENTS_KEY], [comp, twin]);
    qc.setQueryData([...SYSTEMS_KEY], []);
    qc.setQueryData([...LOCATIONS_KEY], []);
    qc.setQueryData([...PRODUCTS_KEY], products);
    qc.setQueryData([...COMPONENT_TYPES_KEY], componentTypes);
    qc.setQueryData([...ME_KEY], me);
    qc.setQueryData([...TAGS_KEY], []);
    window.history.pushState({}, "", "/components/mic-2");
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="/components" component={Components} />
          <Route path="/components/:id" component={Components} />
        </Router>
      </QueryClientProvider>
    ));
    expect(await screen.findByText(/More than one component matches/)).toBeTruthy();
    // Both candidates are offered, not silently one of them.
    expect(screen.getAllByText("Ceiling Mic 2").length).toBe(2);
    // No redirect happened: an ambiguous address is not a resolvable one.
    expect(window.location.pathname).toBe("/components/mic-2");
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

  // Review round 3, regression 1 (task-15-review.md): the new-interface blade
  // addresses its owning component by uuid now (#627 Task 15), but the
  // read-only Component field kept rendering that raw uuid verbatim instead
  // of resolving the component's readable label.
  it("shows the owning component's label, not its uuid, in the new-interface blade's read-only Component field", async () => {
    mount("/components/mic-2");
    await waitFor(() => expect(screen.getByText("Add interface")).toBeTruthy());
    fireEvent.click(screen.getByText("Add interface"));
    await waitFor(() => expect(screen.getByText(/An API on a component/)).toBeTruthy());

    const blades = document.querySelectorAll("aside[data-blade]");
    const top = blades[blades.length - 1] as HTMLElement;
    const field = top.querySelector(".input.input-bordered.flex") as HTMLElement;
    expect(field.textContent).toBe("Ceiling Mic 2");
    expect(field.textContent).not.toBe(comp.id);
  });

  // Review round 3, regression 2 (task-15-review.md): the interface detail
  // blade's refresh() invalidated REACHABILITY_KEY(iface.component), a NAME
  // per the wire (internal/api/interfaces.go), while ReachabilityPanel now
  // keys its read on the component's uuid (#627 review finding 1). A write
  // here silently stopped refreshing the sibling Interfaces panel.
  it("invalidates the reachability read by the owning component's uuid, not its name, after an interface delete", async () => {
    const iface: Interface = { id: uuidFor("iface-1"), name: "ssh", interface_type: "tcp", component: comp.name, component_id: comp.id };
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...COMPONENTS_KEY], [comp]);
    qc.setQueryData([...SYSTEMS_KEY], []);
    qc.setQueryData([...LOCATIONS_KEY], []);
    qc.setQueryData([...PRODUCTS_KEY], products);
    qc.setQueryData([...COMPONENT_TYPES_KEY], componentTypes);
    qc.setQueryData([...ME_KEY], me);
    qc.setQueryData([...TAGS_KEY], []);
    qc.setQueryData([...entityTagsKey("component", "mic-2")], []);
    qc.setQueryData([...INTERFACES_KEY], [iface]);
    qc.setQueryData([...REACHABILITY_KEY(comp.id)], {
      component: comp.id,
      interfaces: [{ interface: iface.name, interface_type: iface.interface_type, verdict: null, layers: [], history: [] }],
    });
    window.history.pushState({}, "", "/components/mic-2");
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="/components" component={Components} />
          <Route path="/components/:id" component={Components} />
        </Router>
      </QueryClientProvider>
    ));
    await waitFor(() => expect(screen.getByLabelText(`Manage ${iface.name}`)).toBeTruthy());
    fireEvent.click(screen.getByLabelText(`Manage ${iface.name}`));
    let top: HTMLElement;
    await waitFor(() => {
      const blades = document.querySelectorAll("aside[data-blade]");
      top = blades[blades.length - 1] as HTMLElement;
      expect(top?.textContent).toContain("Delete");
    });
    const deleteBtn = within(top!).getByText("Delete");

    vi.spyOn(window, "confirm").mockReturnValue(true);
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const { method, url } = req;
      if (method === "DELETE" && url.includes(`/interfaces/${iface.id}`)) {
        return new Response(null, { status: 204 });
      }
      throw new Error(`unexpected fetch in this test: ${method} ${url}`);
    });
    fireEvent.click(deleteBtn);
    // refresh() invalidates in two awaited steps (the interfaces list, then the
    // reachability read), so wait for both rather than stopping at the first.
    await waitFor(() => expect(invalidate.mock.calls.length).toBeGreaterThanOrEqual(2));

    const keys = invalidate.mock.calls.map((c) => JSON.stringify((c[0] as { queryKey: unknown[] }).queryKey));
    expect(keys).toContain(JSON.stringify(REACHABILITY_KEY(comp.id)));
    expect(keys).not.toContain(JSON.stringify(REACHABILITY_KEY(comp.name)));
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
    qc.setQueryData([...COMPONENT_TYPES_KEY], componentTypes);
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
    qc.setQueryData([...COMPONENT_TYPES_KEY], componentTypes);
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
    stubFetch();
    mount("/components/create");
    await waitFor(() => expect(screen.getByText("New component")).toBeTruthy());
    fireEvent.input(screen.getByPlaceholderText("Ceiling Mic 2"), { target: { value: "Spare Panel" } });
    const submit = screen.getByText("Create component").closest("button") as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
    const productSelect = screen.getByLabelText("Product") as HTMLSelectElement;
    fireEvent.change(productSelect, { target: { value: "shure-mxa920" } });
    await waitFor(() => expect(submit.disabled).toBe(false));
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
    stubFetch(async (req) => {
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
    await waitFor(() => expect((screen.getByText("Create component").closest("button") as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByText("Create component"));
    await waitFor(() => expect(sent).toBeTruthy());
    expect((sent as { product: string }).product).toBe("generic-device");
  });
});

// The create form asks WHAT and WHERE first, then shows what the platform will
// name the row. This form already had independent identity fields (#627 Task
// 15d) and was the model the other two moved to; what it lacked was any way to
// SEE the name it was about to be given, so an operator leaving the field blank
// was trusting an affordance they could not read.
describe("Components create identity", () => {
  afterEach(() => window.history.pushState({}, "", "/"));

  const fields = async () => {
    mount("/components/create");
    await waitFor(() => expect(screen.getByText("New component")).toBeTruthy());
    return {
      display: screen.getByPlaceholderText("Ceiling Mic 2") as HTMLInputElement,
      key: screen.getByPlaceholderText("mic-2 (optional)") as HTMLInputElement,
      product: screen.getByLabelText("Product") as HTMLSelectElement,
      submit: screen.getByText("Create component").closest("button") as HTMLButtonElement,
    };
  };

  it("asks what and where before it says anything about the name", async () => {
    await fields();
    const eyebrows = Array.from(document.querySelectorAll("form .eyebrow")).map((e) => e.textContent);
    expect(eyebrows.indexOf("Classification")).toBeLessThan(eyebrows.indexOf("Placement"));
    expect(eyebrows.indexOf("Placement")).toBeLessThan(eyebrows.indexOf("Identity"));
  });

  it("locks the name field on the name the server drafted, ordinal and all", async () => {
    // The reversal (#702). This used to assert "mic-n" and, explicitly, that the
    // field was NEVER "mic-1", because ADR-0104 held the ordinal unknowable
    // before the row existed; reading the lowest free ordinal is not allocating
    // one, so the field carries the name the create is about to use. The stem
    // walk that used to happen here happens in Go now (#695's naming half), so
    // what this pins is the WIRING: the form shows what the route answered.
    stubFetch();
    const { product, key } = await fields();
    // Nothing to lock before a classification is chosen: what comes first is
    // what the rule reads.
    expect(key.readOnly).toBe(false);
    fireEvent.change(product, { target: { value: "shure-mxa920" } });
    await waitFor(() => expect(key.value).toBe("mic-1"));
    // Locked on it, which is the affordance: the platform's answer is the
    // default in effect, not a hint over an empty box. Readonly and never
    // disabled, so the value stays on the keyboard and the field itself is
    // clickable (#657).
    expect(key.readOnly).toBe(true);
    expect(key.disabled).toBe(false);
    // And the sentence beside it says the number is true now rather than held.
    expect(screen.getByText(/1 is the lowest number free here right now/)).toBeTruthy();
  });

  it("asks the route again when the classification moves, and shows the new answer", async () => {
    // What "never suppresses a component's first ordinal" used to assert here,
    // now that the suppression rule lives on the server alone: what this tier
    // can still see is that changing the picker asks a NEW question and the
    // field follows the answer.
    stubFetch();
    const { product, key } = await fields();
    fireEvent.change(product, { target: { value: "generic-app" } });
    await waitFor(() => expect(key.value).toBe("app-1"));
    fireEvent.change(product, { target: { value: "shure-mxa920" } });
    await waitFor(() => expect(key.value).toBe("mic-1"));
  });

  it("shows the placement bucket, and moves it when the placement moves", async () => {
    stubFetch();
    const { product, key } = await fields();
    fireEvent.change(product, { target: { value: "shure-mxa920" } });
    await waitFor(() => expect(key.value).toBe("mic-1"));
    expect(screen.getByText(/Unique among the unplaced components/)).toBeTruthy();

    const location = screen.getByLabelText("Location") as HTMLSelectElement;
    fireEvent.change(location, { target: { value: room.id } });
    await waitFor(() => expect(screen.getByText(/Unique at Boardroom/)).toBeTruthy());

    // A parent wins over a location, the server's own bucket precedence. Both
    // are set here on purpose: with only one, a page handing the two to the
    // rule in the wrong order reads exactly the same.
    const parent = screen.getByLabelText("Parent component") as HTMLSelectElement;
    fireEvent.change(parent, { target: { value: comp.id } });
    await waitFor(() => expect(screen.getByText(/Unique under Ceiling Mic 2/)).toBeTruthy());
    expect(screen.queryByText(/Unique at Boardroom/)).toBeNull();
  });

  it("asks the server for the label, with the same body the create posts, and locks the field on the answer", async () => {
    // The label cannot be rendered here: a rule is a Go template over a closed
    // map (ADR-0098), so the console asks the one engine rather than becoming a
    // second one. What this pins is the WIRING, and the body it asks with,
    // which has to be the body the create posts a moment later or the two
    // describe different rows.
    let asked: Record<string, unknown> | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST" && req.url.includes(":renderLabel")) {
        asked = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({ label: "Ceiling Microphone n", rule: "{{.TypeName}} {{.Ordinal}}" }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      throw new Error(`unexpected fetch in this test: ${req.method} ${req.url}`);
    });
    const { product, display } = await fields();
    fireEvent.change(product, { target: { value: "shure-mxa920" } });
    const location = screen.getByLabelText("Location") as HTMLSelectElement;
    fireEvent.change(location, { target: { value: room.id } });

    await waitFor(() => expect(display.value).toBe("Ceiling Microphone n"));
    expect(display.readOnly).toBe(true);
    // The rule travels with the answer, so the form says where the label came
    // from rather than presenting it as a fact from nowhere.
    expect(screen.getByText(/Rendered from \{\{\.TypeName\}\}/)).toBeTruthy();
    // The name is absent from the question, exactly as it will be from the
    // create body: omitted is "the platform names this".
    expect(asked).toMatchObject({ product: "shure-mxa920", location: room.id });
    expect("name" in asked!).toBe(false);
  });

  it("asks again with the operator's name once they take the pen, since the rule reads it", async () => {
    const bodies: Record<string, unknown>[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST" && req.url.includes(":renderLabel")) {
        const body = JSON.parse(await req.clone().text());
        bodies.push(body);
        return draftJSON(body, "Ceiling Microphone", "{{.TypeName}}");
      }
      throw new Error(`unexpected fetch in this test: ${req.method} ${req.url}`);
    });
    const { product, key } = await fields();
    fireEvent.change(product, { target: { value: "shure-mxa920" } });
    await waitFor(() => expect(bodies.length).toBeGreaterThan(0));
    unlockName();
    fireEvent.input(key, { target: { value: "front-mic" } });
    await waitFor(() => expect(bodies.some((b) => b.name === "front-mic")).toBe(true));
  });

  it("never rewrites the key from the display name", async () => {
    const { display, key } = await fields();
    unlockLabel();
    fireEvent.input(display, { target: { value: "Front Ceiling Mic" } });
    await waitFor(() => expect(display.value).toBe("Front Ceiling Mic"));
    expect(key.value).toBe("");
  });
});

// A create that names a system writes that system's MEMBERSHIP, so the API gates
// it on system:update, the same permission the membership route takes (#707).
// The operator tier holds component:create and no system permission at all, so
// the form must not offer a picker whose use the platform will refuse: #699
// established that a create form does not present what the platform refuses, and
// a 403 discovered after the form is filled in is the outcome that pattern
// exists to prevent.
describe("Components create offers a system only to a principal who may bind one", () => {
  afterEach(() => {
    window.history.pushState({}, "", "/");
    vi.restoreAllMocks();
  });

  // The operator tier as roles.yaml seeds it, beside the all-scoped viewer floor
  // a real principal carries: system:READ is held, system:update is not, which is
  // what makes this test about the permission that gates the membership rather
  // than about visibility.
  const operatorMe: Me = {
    principal: { id: "u-op", kind: "human" },
    human: { username: "op" },
    permissions: ["component:create,update,rename,move", "*:read"],
    grants: [],
  };

  // A principal holding system:update whose grants reach no system at all. That
  // is not a corner case: applicableKinds("system") is {"system"} alone and the
  // cross-tier expansion is unbuilt (#10), so a location-scoped deploy grant
  // resolves system:update to the empty set while the viewer floor beside it
  // reads every system in the estate. Every row comes back readable and none
  // writable, which is what the actions array says.
  const deployMe: Me = {
    principal: { id: "u-dep", kind: "human" },
    human: { username: "tech-east" },
    permissions: ["system:create,update,rename,move", "component:create,update,rename,move", "*:read"],
    grants: [],
  };
  const readOnlySystems: System[] = systems.map((s) => ({ ...s, actions: [] }));

  it("offers the picker to a principal who may write a membership", async () => {
    stubFetch();
    mount("/components/create");
    await waitFor(() => expect(screen.getByText("New component")).toBeTruthy());
    expect(screen.getByLabelText("System")).toBeTruthy();
  });

  // The picker offers what the caller may BIND, which is the update scope and
  // not the read scope. Offering a system the caller can only read is the same
  // defect the permission gate closed, one layer down: the form fills in, the
  // submit is refused, and the operator learns the boundary from a 403.
  it("offers only the systems the caller may update", async () => {
    stubFetch();
    mount("/components/create");
    await waitFor(() => expect(screen.getByText("New component")).toBeTruthy());
    const picker = screen.getByLabelText("System") as HTMLSelectElement;
    const values = [...picker.options].map((o) => o.value);
    expect(values).toContain(uuidFor("s-board"));
    expect(values).not.toContain(uuidFor("s-annex"));
  });

  // The scope half of the gate, which the permission half cannot see.
  it("hides the picker when the permission is held over no system, and says so", async () => {
    stubFetch();
    mount("/components/create", deployMe, readOnlySystems);
    await waitFor(() => expect(screen.getByText("New component")).toBeTruthy());
    expect(screen.queryByLabelText("System")).toBeNull();
    expect(screen.getByText(/scope/)).toBeTruthy();
    // The other placements are untouched: it is the membership that is out of
    // reach, not the create.
    expect(screen.getByLabelText("Location")).toBeTruthy();
    expect(screen.getByLabelText("Parent component")).toBeTruthy();
  });

  it("hides it from a principal who may not, and names the permission", async () => {
    stubFetch();
    mount("/components/create", operatorMe);
    await waitFor(() => expect(screen.getByText("New component")).toBeTruthy());
    expect(screen.queryByLabelText("System")).toBeNull();
    // The rest of the form is untouched: only the membership is out of reach,
    // not the create, and not the other placement the create also binds.
    expect(screen.getByLabelText("Location")).toBeTruthy();
    expect(screen.getByLabelText("Parent component")).toBeTruthy();
    expect(screen.getByText(/system:update/)).toBeTruthy();
  });

  // Both bodies, not just the create's. The form asks :renderLabel with the same
  // shape it will post, so a hidden picker that still carried a system would
  // leak it into the draft first, one request earlier and against a route gated
  // by component:create rather than system:update. stubFetch answers the draft
  // itself and would have swallowed that, so this test drives its own fetch.
  it("neither drafts nor posts a system for the principal it hid the picker from", async () => {
    let sent: Record<string, unknown> | undefined;
    const drafted: Record<string, unknown>[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST" && req.url.includes(":renderLabel")) {
        const body = JSON.parse(await req.clone().text());
        drafted.push(body);
        return draftJSON(body);
      }
      if (req.method === "POST" && req.url.endsWith("/components")) {
        sent = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({ ...comp, id: uuidFor("c-new") }), { status: 201, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ components: [] }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount("/components/create", operatorMe);
    await waitFor(() => expect(screen.getByText("New component")).toBeTruthy());
    fireEvent.change(screen.getByLabelText("Product"), { target: { value: "generic-device" } });
    await waitFor(() => expect((screen.getByText("Create component").closest("button") as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByText("Create component"));
    await waitFor(() => expect(sent).toBeTruthy());
    expect(sent).not.toHaveProperty("system");
    expect(drafted.length).toBeGreaterThan(0);
    for (const body of drafted) expect(body).not.toHaveProperty("system");
  });
});

// #690: the Name column measured 0px at a 1280 viewport, because under
// `table-fixed` a widthless column takes what the declared ones leave, and
// Components declares more than a 1280 screen has to give. The identifier an
// operator scans was the first thing to vanish, and Tags kept every pixel.
//
// The assertion is on the DOM rather than on the descriptor, and it is both
// halves of the fix at once: Name still declares NO width (so it absorbs a wide
// screen, which is the behaviour worth keeping), and the table asks for a floor
// under it, so a narrow screen scrolls the card sideways instead of squeezing
// the column out. The same test rides on Systems and Locations, which is where
// "all three behave the same way" is actually asserted: today they do not.
describe("Components list keeps a floor under the Name column (#690)", () => {
  it("declares no width on Name and a table floor that leaves it NAME_MIN_W", async () => {
    localStorage.clear();
    mount("/components");
    await waitFor(() => expect(document.querySelector("table.og-rows")).toBeTruthy());

    const table = document.querySelector("table.og-rows") as HTMLTableElement;
    const cols = [...table.querySelectorAll("colgroup col")] as HTMLTableColElement[];
    const declared = cols.slice(1).reduce((sum, c) => sum + parseInt(c.style.width || "0", 10), 0);

    expect(cols[0].style.width).toBe("");
    expect(parseInt(table.style.minWidth, 10) - declared).toBe(NAME_MIN_W);
  });
});
