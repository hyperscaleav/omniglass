import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor, fireEvent } from "@solidjs/testing-library";
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

// The Components route in the create-as-route model (the list is Explore's outline, #861): New routes
// to /components/create (a draft accordion), Save hands off to /components/<name> in
// edit; the detail is read-only in view (no in-body mutation control) and editable via
// the pencil. Data is seeded into the query cache so no server is needed; `>` grants
// every permission.
const me: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const comp: Component = { id: uuidFor("c-1"), name: "mic-2", label: "Ceiling Mic 2", product_id: "shure-mxa920", system_count: 0, effective_tags: {} };

// #614: every component is an instance of a product; the create form's
// Product picker offers the real catalog plus the three generics that make
// the classification floor total for anything not yet modeled more
// specifically.
const products: Product[] = [
  { id: uuidFor("prod-shure"), name: "shure-mxa920", label: "Shure MXA920", kind: "device", component_type: "ceiling-mic", component_type_id: uuidFor("ct-ceiling-mic"), official: true },
  { id: uuidFor("prod-generic-device"), name: "generic-device", label: "Generic Device", kind: "device", component_type: "generic-device", component_type_id: uuidFor("ct-generic-device"), official: true },
  { id: uuidFor("prod-generic-app"), name: "generic-app", label: "Generic App", kind: "app", component_type: "generic-app", component_type_id: uuidFor("ct-generic-app"), official: true },
  { id: uuidFor("prod-generic-service"), name: "generic-service", label: "Generic Service", kind: "service", component_type: "generic-service", component_type_id: uuidFor("ct-generic-service"), official: true },
];

// The device-class registry the create form resolves a generated name's stem
// through. Three tiers on purpose: ceiling-mic sets no stem of its own, so a
// preview that read the product's own type and stopped would answer nothing
// where the server's inheriting walk answers "mic".
const componentTypes: ComponentType[] = [
  { id: uuidFor("ct-device"), name: "device", label: "Device", official: true, forked: false, stem: "device", default_tags: [] },
  { id: uuidFor("ct-mic"), name: "mic", label: "Microphone", official: true, forked: false, parent: "device", stem: "mic", default_tags: [] },
  { id: uuidFor("ct-ceiling-mic"), name: "ceiling-mic", label: "Ceiling Microphone", official: true, forked: false, parent: "mic", default_tags: [] },
  { id: uuidFor("ct-generic-device"), name: "generic-device", label: "Generic Device", official: true, forked: false, stem: "device", default_tags: [] },
  { id: uuidFor("ct-generic-app"), name: "generic-app", label: "Generic App", official: true, forked: false, stem: "app", default_tags: [] },
  { id: uuidFor("ct-generic-service"), name: "generic-service", label: "Generic Service", official: true, forked: false, stem: "service", default_tags: [] },
];

// One room, so the create form's placement has both a parent candidate and a
// location candidate: the bucket precedence (a parent wins) cannot be witnessed
// with only one of the two set, and a page passing the two the wrong way round
// is invisible to a unit test of the rule.
const room: Location = { id: uuidFor("l-room"), name: "boardroom", label: "Boardroom", location_type: "room", effective_tags: {} };

// Two systems, told apart by their ACTIONS rather than by anything an operator
// reads (#707 review). actions is the server's per-row answer computed from the
// same per-action scope the gateway enforces, so "which systems may this caller
// bind" is data the console filters, never a scope it resolves. board carries
// update; annex is readable and not writable, which is what a location-scoped
// deploy grant beside the viewer floor produces for every system in the fleet.
const systems: System[] = [
  { id: uuidFor("s-board"), name: "board", label: "Boardroom AV", member_count: 0, actions: ["create", "update", "delete"] },
  { id: uuidFor("s-annex"), name: "annex", label: "Annex AV", member_count: 0, actions: [] },
];

function mount(path: string, who: Me = me, sys: System[] = systems, comps: Component[] = [comp]) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...COMPONENTS_KEY], comps);
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
  fireEvent.click(screen.getByRole("button", { name: "Override the label" }));
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
    // The label is filled in (an operator label, independent of the
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
    expect(body.label).toBe("Ceiling Mic 9");
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

  it("never rewrites the key from the label", async () => {
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
  // reads every system in the fleet. Every row comes back readable and none
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

// The label pen on the edit blade (#693). The "Generated" chip left the lists,
// and the fact landed beside the field an operator can act on. This block proves
// the page is WIRED to the shared field (components/LabelPenField.test.tsx
// proves the field's own contract) and proves what a Save posts.
//
// Components is also where the two pens sit on one blade, which is the case
// worth holding: the NAME's chip stays, in the read state beside the name, and
// the LABEL's is the lock in edit. A change that put the label's word back in a
// badge would make the two indistinguishable again.
