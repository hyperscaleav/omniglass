import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor, fireEvent } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Locations from "./Locations";
import { LOCATIONS_KEY, type Location } from "../lib/locations";
import { LOCATION_TYPES_KEY, type LocationType } from "../lib/location_types";
import { ownerPropertiesKey, type EffectiveProperty } from "../lib/owner_properties";
import { ME_KEY, type Me } from "../lib/auth";
import { TAGS_KEY, entityTagsKey } from "../lib/tags";
import { uuidFor } from "../lib/testids";

// The Locations route in the create-as-route model (the list is Explore's outline, #861): New routes
// to /locations/create (a draft accordion), Save hands off to /locations/<name> in
// edit; the detail is read-only in view (no in-body mutation control) and editable
// via the pencil. The detail also carries the Properties panel, which resolves the
// location type's declared-property contract against the location's own values.
// Data is seeded into the query cache so no server is needed; `>` grants every
// permission.
const me: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const hq: Location = { id: uuidFor("l-hq"), name: "hq", label: "HQ", location_type: "campus", effective_tags: {} };
const lab: Location = { id: uuidFor("l-lab"), name: "lab", label: "Lab", location_type: "campus", effective_tags: {} };
const hqB1: Location = { id: uuidFor("l-b1"), name: "hq-b1", label: "HQ B1", location_type: "building", parent: "hq", parent_id: hq.id, effective_tags: {} };
// Registry rows carry a uuid id and the name in name (ADR-0062); the
// server stores and compares the handle everywhere a location references its
// type, so a fixture with the handle in the id slot would hide a uuid-vs-name
// join bug (that is how #466 shipped).
const types: LocationType[] = [
  { id: uuidFor("lt-campus"), name: "campus", label: "Campus", icon: "landmark", official: true, forked: false, allowed_parent_types: ["root"] },
  { id: uuidFor("lt-building"), name: "building", label: "Building", icon: "building", official: true, forked: false, allowed_parent_types: ["root", "campus"] },
  // Unconstrained: any parent. Exists so the self-exclusion test below cannot
  // lean on the allowed-parents filter to hide the node's own subtree.
  { id: uuidFor("lt-area"), name: "area", label: "Area", icon: "map-pin", official: false, forked: false, allowed_parent_types: [] },
  // The one type here that NAMES its own rows (#687): its rule is what the
  // create form previews, and its absence on the three above is what makes them
  // the operator-named case in the same fixture.
  { id: uuidFor("lt-room"), name: "room", label: "Room", icon: "door-open", official: false, forked: false, allowed_parent_types: [], name_rule: { stem: "room", bare_first: true, examples: ["room", "room-2"] } },
];
// The campus type's contract, resolved against hq: one inherited default, plus one
// value hq sets that no contract declares.
const hqProperties: EffectiveProperty[] = [
  { property_type_name: "site.timezone", property_type_id: "site.timezone-id", label: "Time zone", data_type: "string", required: false, is_set: false, from_contract: true, default_value: "UTC", value: "UTC" },
  { property_type_name: "site.note", property_type_id: "site.note-id", label: "Note", data_type: "string", required: false, is_set: true, from_contract: false, set_value: "leased", value: "leased", value_id: "v-note" },
];

function mount(path: string, extraLocations: Location[] = [], meOverride: Me = me, registry: LocationType[] = types, base: Location[] = [hq, lab, hqB1]) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  const all = [...base, ...extraLocations];
  qc.setQueryData([...LOCATIONS_KEY], all);
  qc.setQueryData([...LOCATION_TYPES_KEY], registry);
  qc.setQueryData([...ME_KEY], meOverride);
  qc.setQueryData([...TAGS_KEY], []);
  // Keyed by uuid (#627 review finding 1): the detail page's panels now
  // address by the location's id, not its name.
  for (const l of all) qc.setQueryData([...entityTagsKey("location", l.id)], []);
  // Seed every location's effective properties so the detail's panel resolves
  // from cache (the tests that fake fetch refuse any request they did not expect).
  for (const l of all) qc.setQueryData([...ownerPropertiesKey("location", l.id)], l.name === "hq" ? hqProperties : []);
  window.history.pushState({}, "", path);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="/locations" component={Locations} />
        <Route path="/locations/:id" component={Locations} />
        <Route path="/explore" component={() => <div data-testid="explore-page" />} />
      </Router>
    </QueryClientProvider>
  ));
}

// The server's draft answer, which is where the locked NAME comes from since
// #702. The console used to read the chosen type's name_rule and write the
// token "n" where the ordinal went; it shows what this returns, ordinal and
// all. The name below is a fixture rather than a second implementation of the
// mint: that the gateway mints "room" for a suppressing rule is proven against
// a real database in internal/storage, and what this file proves is that the
// form shows what the gateway said and posts the number back.
function draftJSON(body: { location_type?: string; name?: string }, label = "", rule = "") {
  const drafted = body.name
    ? { name: body.name, label, rule }
    : { name: body.location_type === "room" ? "room" : "thing-1", ordinal: 1, label, rule };
  return new Response(JSON.stringify(drafted), { status: 200, headers: { "Content-Type": "application/json" } });
}

// A type with no name rule is a REFUSAL from the route, located on body.name,
// which is how the form tells "the platform will not name this" from every
// other 422 it can get.
function noRuleJSON() {
  return new Response(
    JSON.stringify({
      status: 422,
      detail: "this location_type has no name rule, so the platform cannot generate a name for it",
      errors: [{ location: "body.name", message: "no name rule" }],
    }),
    { status: 422, headers: { "Content-Type": "application/json" } },
  );
}

function stubFetch(rest?: (req: Request) => Promise<Response> | Response) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const req = input as Request;
    if (req.method === "POST" && req.url.includes(":renderLabel")) {
      const body = JSON.parse(await req.clone().text());
      return body.name || body.location_type === "room" ? draftJSON(body) : noRuleJSON();
    }
    if (rest) return rest(req);
    throw new Error(`unexpected fetch in this test: ${req.method} ${req.url}`);
  });
}

describe("Locations create-as-route", () => {
  afterEach(() => window.history.pushState({}, "", "/"));

  it("renders the draft-create accordion at /locations/create", async () => {
    mount("/locations/create");
    await waitFor(() => expect(screen.getByText("New location")).toBeTruthy());
    expect(screen.getByText("Draft")).toBeTruthy();
    expect(screen.getByText("Create location")).toBeTruthy();
    // Identity + Placement fields present; the binding sections are locked.
    expect(screen.getByText("Identity")).toBeTruthy();
    expect(screen.getByText("Placement")).toBeTruthy();
    expect(screen.getByText("Name")).toBeTruthy();
    expect(screen.getByText("Location type")).toBeTruthy();
    expect(screen.getByText("Parent")).toBeTruthy();
    expect(screen.getByText(/Available once the location is created/)).toBeTruthy();
  });

  it("leads back to Explore, where every place is listed now (#861)", async () => {
    mount("/locations/create");
    fireEvent.click(await screen.findByRole("button", { name: /Explore/ }));
    expect(await screen.findByTestId("explore-page")).toBeTruthy();
  });

  it("posts the location_type handle, never the uuid, on create (#466)", async () => {
    let captured: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST" && req.url.endsWith("/locations")) {
        captured = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({ id: uuidFor("l-annex"), name: "annex", location_type: "campus" }), { status: 201, headers: { "Content-Type": "application/json" } });
      }
      throw new Error(`unexpected fetch in this test: ${req.method} ${req.url}`);
    });
    mount("/locations/create");
    await waitFor(() => expect(screen.getByText("New location")).toBeTruthy());
    fireEvent.input(screen.getByPlaceholderText("Conf Room 301"), { target: { value: "Annex" } });
    // Typed, not derived (#688): campus carries no name rule, so nothing will
    // mint a name here and the operator supplies one. Before this slice the
    // label above filled this field in on its own.
    fireEvent.input(screen.getByPlaceholderText("boardroom"), { target: { value: "annex" } });
    const typeSelect = screen.getByText("Select a type…").closest("select") as HTMLSelectElement;
    // Pick the first real option (index 0 is the disabled placeholder); the
    // assertion below pins what its selection posts.
    fireEvent.change(typeSelect, { target: { value: typeSelect.options[1].value } });
    fireEvent.click(screen.getByText("Create location"));
    await waitFor(() => expect(captured).toBeTruthy());
    // The server resolves the name (storage joins location_type by
    // name); a uuid here inserts NULL and the create 500s on a live install.
    expect((captured as { location_type: string }).location_type).toBe("campus");
  });

});

// The create form asks WHAT and WHERE first, then shows what the platform will
// name the row. The derivation this block used to pin (type a label, watch
// the key fill itself in) was removed in #688: a blank name is now the REQUEST to
// generate one from the location_type's name rule, so deriving one claimed the
// platform's pen the moment an operator typed a label, and the always-required
// name gate meant the console could never reach the generator at all.
//
// What each tier can witness, which is worth being precise about:
//
//   - THIS file proves the page is WIRED to the shared section and to the mint.
//     Drop either and the tests below fail.
//   - components/CreateIdentity.test.tsx proves the section's own contract, and
//     lib/namegen.test.ts the shape and bucket rules. Neither can tell you a page
//     forgot to use them.
// Both identity fields open LOCKED on the platform's answer (#699), so a test
// that means to type into one takes the pen first, exactly as an operator does.
// The lock is a square icon button inside each field's join and carries no text
// (#657), so each is addressed by its accessible name; where the name generates
// nothing there is only the label's.
function unlockName() {
  fireEvent.click(screen.getByRole("button", { name: "Override the name" }));
}
function unlockLabel() {
  fireEvent.click(screen.getByRole("button", { name: "Override the label" }));
}

describe("Locations create identity", () => {
  afterEach(() => window.history.pushState({}, "", "/"));

  const fields = async () => {
    mount("/locations/create");
    await waitFor(() => expect(screen.getByText("New location")).toBeTruthy());
    const display = screen.getByPlaceholderText("Conf Room 301") as HTMLInputElement;
    const key = screen.getByPlaceholderText("boardroom") as HTMLInputElement;
    const typeSelect = screen.getByText("Select a type…").closest("select") as HTMLSelectElement;
    const submit = screen.getByText("Create location").closest("button") as HTMLButtonElement;
    return { display, key, typeSelect, submit };
  };

  it("never rewrites the key from the label", async () => {
    const { display, key } = await fields();
    unlockLabel();
    fireEvent.input(display, { target: { value: "Conf Room 301" } });
    // The old behaviour filled this in with "conf-room-301".
    await waitFor(() => expect(display.value).toBe("Conf Room 301"));
    expect(key.value).toBe("");
    expect(screen.queryByText(/Derived from the label/)).toBeNull();
  });

  it("shows what the platform will name it once a generating type is chosen", async () => {
    stubFetch();
    const { typeSelect, key } = await fields();
    // Nothing to lock before a classification is chosen: what comes first is
    // what the rule reads.
    expect(key.readOnly).toBe(false);
    fireEvent.change(typeSelect, { target: { value: "room" } });
    await waitFor(() => expect(key.value).toBe("room"));
    expect(key.readOnly).toBe(true);
    expect(key.disabled).toBe(false);
    // A suppressing rule's first name IS the bare stem, and the number beside
    // it is 1. This used to assert a warning that the NEXT one would be
    // "room-2", which the form needed while it was showing a shape; the shown
    // value is the name the create will use, and the case the warning existed
    // for is the one the precondition refuses (#702).
    expect(screen.getByText(/1 is the lowest number free here right now/)).toBeTruthy();
  });

  it("shows the placement path the name has to be unique in, as context and not as a prefix", async () => {
    stubFetch();
    const { typeSelect, key } = await fields();
    fireEvent.change(typeSelect, { target: { value: "room" } });
    await waitFor(() => expect(key.value).toBe("room"));
    expect(screen.getByText(/Unique at the fleet root/)).toBeTruthy();

    const parentSelect = screen.getByText("Root (no parent)").closest("select") as HTMLSelectElement;
    fireEvent.change(parentSelect, { target: { value: hqB1.id } });
    await waitFor(() => expect(screen.getByText(/Unique under HQ \/ HQ B1/)).toBeTruthy());
    // Context only: the path never lands in the field the operator types into,
    // which still holds the name the route drafted.
    expect(key.value).toBe("room");
  });

  it("shows the label the shipped location rule renders, and names the rule", async () => {
    // The default state of the location create form as of #657: the global
    // location rule reads the name as words and titles it, so the locked field
    // carries a real value and the form says which rule produced it. The render
    // is the server's (the browser never re-implements the engine), so the fetch
    // is mocked with what the route actually answers.
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST" && req.url.includes(":renderLabel")) {
        return draftJSON(JSON.parse(await req.clone().text()), "North Boardroom", "{{title (words .Name)}}");
      }
      throw new Error(`unexpected fetch in this test: ${req.method} ${req.url}`);
    });
    const { typeSelect, display } = await fields();
    fireEvent.change(typeSelect, { target: { value: "room" } });
    await waitFor(() => expect(display.value).toBe("North Boardroom"));
    expect(display.readOnly).toBe(true);
    expect(screen.getByText(/Rendered from/)).toBeTruthy();
  });

  it("falls back to the name in the locked label field where no rule resolves", async () => {
    // Reachable by clearing the rule at every tier, and it stopped being the
    // shipped fleet's default when the location rule landed (#657). A locked
    // field showing nothing at all would be worse than the form this replaces,
    // so it shows what an operator will actually read, which is the name.
    stubFetch();
    const { typeSelect, display } = await fields();
    fireEvent.change(typeSelect, { target: { value: "room" } });
    await waitFor(() => expect(screen.getByText(/No label rule applies/)).toBeTruthy());
    expect(display.value).toBe("room");
    expect(display.readOnly).toBe(true);
  });

  it("lets a nameless create through for a generating type, and refuses one without a rule", async () => {
    // The refusal is the SERVER's now, read off the field it is located on
    // rather than off a null the browser computed from the type's rule (#702).
    stubFetch();
    const { typeSelect, submit } = await fields();
    fireEvent.change(typeSelect, { target: { value: "campus" } });
    // Campus names nothing, so a name is the operator's to supply. The wait is
    // on the REFUSAL rather than on the disabled button: the button is disabled
    // from the first frame, so waiting on it would pass before the route had
    // answered anything at all.
    await waitFor(() => expect(screen.getByText(/no name rule/)).toBeTruthy());
    expect(submit.disabled).toBe(true);

    fireEvent.change(typeSelect, { target: { value: "room" } });
    await waitFor(() => expect(submit.disabled).toBe(false));
  });

  it("omits the name from the POST body when it is left blank", async () => {
    let captured: Record<string, unknown> | undefined;
    stubFetch(async (req) => {
      if (req.method === "POST" && req.url.endsWith("/locations")) {
        captured = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({ id: uuidFor("l-new"), name: "room", location_type: "room" }), { status: 201, headers: { "Content-Type": "application/json" } });
      }
      throw new Error(`unexpected fetch in this test: ${req.method} ${req.url}`);
    });
    const { typeSelect, display, key } = await fields();
    fireEvent.change(typeSelect, { target: { value: "room" } });
    await waitFor(() => expect(key.value).toBe("room"));
    // A label is typed and the name is left locked: the two pens are
    // independent, and a locked name has to be OMITTED rather than posted as
    // "", which the API refuses against the entity-name pattern.
    unlockLabel();
    fireEvent.input(display, { target: { value: "Conf Room 301" } });
    fireEvent.click(screen.getByText("Create location"));
    await waitFor(() => expect(captured).toBeTruthy());
    expect("name" in captured!).toBe(false);
    // The NAME the locked field was showing goes back as the precondition,
    // which is the one thing a locked field DOES post (#702).
    expect(captured!.expected_name).toBe("room");
    expect(captured!.label).toBe("Conf Room 301");
  });

  it("sends an override verbatim and leaves the label alone", async () => {
    let captured: Record<string, unknown> | undefined;
    stubFetch(async (req) => {
      if (req.method === "POST" && req.url.endsWith("/locations")) {
        captured = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({ id: uuidFor("l-new"), name: "war-room", location_type: "room" }), { status: 201, headers: { "Content-Type": "application/json" } });
      }
      throw new Error(`unexpected fetch in this test: ${req.method} ${req.url}`);
    });
    const { typeSelect, display, key } = await fields();
    fireEvent.change(typeSelect, { target: { value: "room" } });
    await waitFor(() => expect(key.value).toBe("room"));
    unlockLabel();
    fireEvent.input(display, { target: { value: "War Room" } });
    unlockName();
    fireEvent.input(key, { target: { value: "war-room" } });
    // Overriding one does not disturb the other.
    expect(display.value).toBe("War Room");
    fireEvent.click(screen.getByText("Create location"));
    await waitFor(() => expect(captured).toBeTruthy());
    expect(captured!.name).toBe("war-room");
    // And no precondition beside it: an operator-typed name allocates no
    // ordinal, so posting one would be a claim nothing can check (a 422).
    expect("expected_name" in captured!).toBe(false);
    expect(captured!.label).toBe("War Room");
  });
});

// The label pen on the edit blade (#693). The chip left the list, and the fact
// landed on the field an operator can act on. This block proves the page is
// WIRED to the shared field (components/LabelPenField.test.tsx proves the
// field's own contract) and proves the half no component test can see: what a
// Save actually posts.
// The edit face is a URL fact: ?edit=1 on the detail route requests edit mode, and
// leaving edit strips it. The one-shot in-memory handoff (pendingedit) is gone, so
// a deep link, a refresh mid-edit, and the create/pencil handoffs all express the
// mode in the URL itself.
// Slice 2 of #800: the identity route with ?edit=1 renders the ZOOM (whose
// configure face receives the intent), not the classic face; only
// ?view=detail still reaches the classic editor until slice 3 retires it.
describe("?edit=1 stops routing to the classic face (#800)", () => {
  it("lands on the location zoom with the configure face editing", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...ME_KEY], me);
    window.history.pushState({}, "", `/locations/${hq.id}?edit=1`);
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="/locations/:id" component={Locations} />
        </Router>
      </QueryClientProvider>
    ));
    // The zoom mounts (its skeleton is fine: the routing is the assertion);
    // the classic face's identity accordion must NOT appear.
    await waitFor(() => expect(document.querySelector('[data-testid="configure-face"], .skeleton')).toBeTruthy());
    expect(screen.queryByText("PLACEMENT")).toBeNull();
  });
});


// Slice 3 of #800: the classic face retires. ?view=detail stops being an
// address; the identity route renders the zoom whatever the params say, and
// only the reserved create segment keeps this page's own shell.
describe("the classic face is gone (#800)", () => {
  it("?view=detail renders the zoom, not the classic accordion", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...ME_KEY], me);
    window.history.pushState({}, "", `/locations/${hq.id}?view=detail`);
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="/locations/:id" component={Locations} />
        </Router>
      </QueryClientProvider>
    ));
    await waitFor(() => expect(document.querySelector('[data-testid="tab-rail"], .skeleton')).toBeTruthy());
    expect(screen.queryByText("PLACEMENT")).toBeNull();
    expect(screen.queryByRole("button", { name: /^cancel$/i })).toBeNull();
  });
});

