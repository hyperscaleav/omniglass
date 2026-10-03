import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor, fireEvent } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Systems from "./Systems";
import { SYSTEMS_KEY, type System } from "../lib/systems";
import { LOCATIONS_KEY } from "../lib/locations";
import { COMPONENTS_KEY } from "../lib/components";
import { STANDARDS_KEY, type Standard } from "../lib/standards";
import { SYSTEM_TYPES_KEY, type SystemType } from "../lib/system_types";
import { ownerPropertiesKey, type EffectiveProperty } from "../lib/owner_properties";
import { ME_KEY, type Me } from "../lib/auth";
import { TAGS_KEY, entityTagsKey } from "../lib/tags";
import { uuidFor } from "../lib/testids";

// The Systems route in the create-as-route model (the list is Explore's outline, #861): New routes
// to /systems/create (a draft accordion), Save hands off to /systems/<id> in edit;
// the detail is read-only in view (no in-body mutation control) and editable via the
// pencil. A system conforms to a STANDARD, whose declared-property contract the
// detail's Properties panel resolves. Data is seeded into the query cache so no
// server is needed; `>` grants every permission.
const me: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const sys: System = { id: uuidFor("s-1"), name: "boardroom", label: "Boardroom", standard: "meeting-room", standard_id: uuidFor("std-meeting-room"), system_type: "class", system_type_id: uuidFor("st-class"), member_count: 2, effective_tags: {} };
// The coarse space taxonomy (ADR-0096), nested: the picker renders it as a
// tree, not a flat list, so the fixture is two levels deep. Deliberately NOT
// named "Boardroom": the system's own label already is, and a collision
// would make the type assertions pass on the wrong element.
const systemTypes: SystemType[] = [
  { id: uuidFor("st-av"), name: "av", label: "AV", official: true, stem: "av", abbrev: "av", icon: "layers" },
  { id: uuidFor("st-room"), name: "room", label: "Room", official: true, parent: "av", parent_id: uuidFor("st-av"), stem: "room", abbrev: "rm", icon: "door-open" },
  { id: uuidFor("st-class"), name: "class", label: "Classroom", official: true, parent: "room", parent_id: uuidFor("st-room"), stem: "classroom", abbrev: "cls" },
];
const standards: Standard[] = [
  { id: uuidFor("meeting-room"), name: "meeting-room", label: "Meeting room", official: true },
  { id: uuidFor("huddle-space"), name: "huddle-space", label: "Huddle space", official: false },
];
// The standard's contract, resolved against the system: one inherited default and
// one value the system sets directly with nothing declaring it.
const properties: EffectiveProperty[] = [
  { property_type_name: "seat_count", property_type_id: "seat_count-id", label: "Seat count", data_type: "int", required: false, is_set: false, from_contract: true, default_value: 12, value: 12 },
  { property_type_name: "room.note", property_type_id: "room.note-id", label: "Note", data_type: "string", required: false, is_set: true, from_contract: false, set_value: "corner room", value: "corner room", value_id: "v-note" },
];

function mount(path: string, systems: System[] = [sys]) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...SYSTEMS_KEY], systems);
  qc.setQueryData([...LOCATIONS_KEY], []);
  qc.setQueryData([...COMPONENTS_KEY], []);
  qc.setQueryData([...STANDARDS_KEY], standards);
  qc.setQueryData([...SYSTEM_TYPES_KEY], systemTypes);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...TAGS_KEY], []);
  // Keyed by uuid (#627 review finding 1): the detail page's panels now
  // address by the system's id, not its name.
  for (const s of systems) {
    qc.setQueryData([...ownerPropertiesKey("system", s.id)], s.id === sys.id ? properties : []);
    qc.setQueryData([...entityTagsKey("system", s.id)], []);
  }
  window.history.pushState({}, "", path);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="/systems" component={Systems} />
        <Route path="/systems/:id" component={Systems} />
      </Router>
    </QueryClientProvider>
  ));
}

describe("Systems create-as-route", () => {
  afterEach(() => window.history.pushState({}, "", "/"));

  it("renders the draft-create accordion at /systems/create", async () => {
    mount("/systems/create");
    await waitFor(() => expect(screen.getByText("New system")).toBeTruthy());
    expect(screen.getByText("Draft")).toBeTruthy();
    expect(screen.getByText("Create system")).toBeTruthy();
    // Identity + Placement fields present; the binding sections are locked.
    expect(screen.getByText("Name")).toBeTruthy();
    expect(screen.getByText("Standard")).toBeTruthy();
    expect(screen.getByText(/Available once the system is created/)).toBeTruthy();
  });

  it("offers the standard registry on the create form, with a one-off option", async () => {
    mount("/systems/create");
    // The label is associated with its control by id (FieldRow), not by wrapping
    // it, so the lookup goes through the accessible name. That also proves the
    // association, which the old markup did not.
    const picker = (await waitFor(() => screen.getByLabelText("Standard"))) as HTMLSelectElement;
    // Conforming to no standard is first class, so it heads the list.
    expect(Array.from(picker.options).map((o) => o.value)).toEqual(["", "huddle-space", "meeting-room"]);
  });

  it("offers the system_type tree on the create form, indented, with an unclassified option", async () => {
    mount("/systems/create");
    const picker = (await waitFor(() => screen.getByLabelText("Type"))) as HTMLSelectElement;
    // Unclassified heads the list (the column is nullable), then the tree in
    // depth-first order, each level indented by a non-breaking-space run. The
    // indent is the assertion that this is the TREE picker and not a flat
    // <select> over the same rows.
    expect(Array.from(picker.options).map((o) => o.value)).toEqual(["", "av", "room", "class"]);
    const labels = Array.from(picker.options).map((o) => o.textContent);
    expect(labels[1]).toBe("AV");
    expect(labels[2]).toBe("\u00A0\u00A0\u00A0Room");
    expect(labels[3]).toBe("\u00A0\u00A0\u00A0\u00A0\u00A0\u00A0Classroom");
  });

});

// The Properties panel on the system detail is the shared owner panel, pointed at
// the system arc: the standard's contract resolved against the system's own values,
// with anything the system sets that no contract declares grouped off contract.
// The create form asks WHAT and WHERE first, then shows what the platform will
// name the row. This form derived the name from the label until #688 and
// refused to submit without one, which together made the system-tier generator
// (#686) unreachable from the console: every console-created system arrived with
// an operator-owned name whether the operator meant that or not.
// Both identity fields open LOCKED on the platform's answer (#699), so a test
// that means to type into one takes the pen first, exactly as an operator does.
// The lock is a square icon button inside the field's join and carries no text
// (#657), so it is addressed by its accessible name.
function unlockLabel() {
  fireEvent.click(screen.getByRole("button", { name: "Override the label" }));
}

// The server's draft answer, which is where the locked NAME comes from since
// #702: the console used to walk the system_type chain for a stem and write the
// token "n" where the ordinal went. The names below are fixtures rather than a
// second implementation of the mint (the suppression rule is ADR-0101's and is
// proven against a real database in internal/storage); what this file proves is
// that the form shows what the route answered and posts the number back.
//
// An unclassified system is a REFUSAL located on body.name, which is how the
// form tells "the platform will not name this" from every other 422.
const DRAFTED: Record<string, string> = { class: "classroom", room: "room", board: "boardroom" };

function draftJSON(body: { system_type_id?: string; name?: string }, label = "", rule = "") {
  const drafted = body.name
    ? { name: body.name, label, rule }
    : { name: DRAFTED[body.system_type_id ?? ""] ?? "thing", ordinal: 1, label, rule };
  return new Response(JSON.stringify(drafted), { status: 200, headers: { "Content-Type": "application/json" } });
}

function unclassifiedJSON() {
  return new Response(
    JSON.stringify({
      status: 422,
      detail: "a system with no system_type has no stem to generate a name from",
      errors: [{ location: "body.name", message: "unclassified" }],
    }),
    { status: 422, headers: { "Content-Type": "application/json" } },
  );
}

function stubFetch(rest?: (req: Request) => Promise<Response> | Response) {
  return vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const req = input as Request;
    if (req.method === "POST" && req.url.includes(":renderLabel")) {
      const body = JSON.parse(await req.clone().text());
      return body.name || body.system_type_id ? draftJSON(body) : unclassifiedJSON();
    }
    if (rest) return rest(req);
    throw new Error(`unexpected fetch in this test: ${req.method} ${req.url}`);
  });
}

describe("Systems create identity", () => {
  afterEach(() => window.history.pushState({}, "", "/"));

  const fields = async () => {
    mount("/systems/create");
    await waitFor(() => expect(screen.getByText("New system")).toBeTruthy());
    return {
      display: screen.getByPlaceholderText("Executive Boardroom") as HTMLInputElement,
      key: screen.getByPlaceholderText("exec-boardroom") as HTMLInputElement,
      type: screen.getByLabelText("Type") as HTMLSelectElement,
      submit: screen.getByText("Create system").closest("button") as HTMLButtonElement,
    };
  };

  it("never rewrites the key from the label", async () => {
    const { display, key } = await fields();
    unlockLabel();
    fireEvent.input(display, { target: { value: "Executive Boardroom" } });
    await waitFor(() => expect(display.value).toBe("Executive Boardroom"));
    expect(key.value).toBe("");
    expect(screen.queryByText(/Derived from the label/)).toBeNull();
  });

  it("locks the name field on the name the server drafted for the chosen type", async () => {
    // What this used to assert was the SHAPE the browser resolved ("classroom",
    // with a warning that the next one would be "classroom-2"), because the
    // ordinal was held unknowable. It shows the name the create will use, and
    // the ordinal beside it, drafted by the one generator (#702).
    stubFetch();
    const { type, key } = await fields();
    // Unclassified is the default and generates nothing, so there is no lock to
    // show until a type is chosen.
    expect(key.readOnly).toBe(false);
    fireEvent.change(type, { target: { value: "class" } });
    await waitFor(() => expect(key.value).toBe("classroom"));
    expect(key.readOnly).toBe(true);
    expect(key.disabled).toBe(false);
    expect(screen.getByText(/1 is the lowest number free here right now/)).toBeTruthy();
    expect(screen.getByText(/Unique among the unplaced systems/)).toBeTruthy();
  });

  it("asks again when the type moves, so the shown name follows the classification", async () => {
    // The stem walk this used to pin in the browser is the gateway's alone now
    // (#695's naming half). What this tier can still see is that the question is
    // re-asked and the field follows the answer.
    stubFetch();
    const { type, key } = await fields();
    fireEvent.change(type, { target: { value: "room" } });
    await waitFor(() => expect(key.value).toBe("room"));
    fireEvent.change(type, { target: { value: "class" } });
    await waitFor(() => expect(key.value).toBe("classroom"));
  });

  it("requires a name only for an unclassified system, which has no stem", async () => {
    stubFetch();
    const { submit, type, key } = await fields();
    // Unclassified is the default, and it is the one the gateway refuses to
    // name (ErrSystemTypeRequiredForName). The refusal is the SERVER's since
    // #702, so the wait is on it landing rather than on a button that is
    // disabled from the first frame either way.
    await waitFor(() => expect(screen.getByText(/This system is unclassified or its type chain sets no stem/)).toBeTruthy());
    expect(submit.disabled).toBe(true);
    fireEvent.input(key, { target: { value: "one-off" } });
    await waitFor(() => expect(submit.disabled).toBe(false));

    fireEvent.input(key, { target: { value: "" } });
    fireEvent.change(type, { target: { value: "class" } });
    await waitFor(() => expect(submit.disabled).toBe(false));
  });

  it("omits the name from the POST body when it is left blank", async () => {
    let captured: Record<string, unknown> | undefined;
    stubFetch(async (req) => {
      if (req.method === "POST" && req.url.endsWith("/systems")) {
        captured = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({ id: uuidFor("s-new"), name: "classroom" }), { status: 201, headers: { "Content-Type": "application/json" } });
      }
      throw new Error(`unexpected fetch in this test: ${req.method} ${req.url}`);
    });
    const { type, display, key } = await fields();
    fireEvent.change(type, { target: { value: "class" } });
    await waitFor(() => expect(key.value).toBe("classroom"));
    unlockLabel();
    fireEvent.input(display, { target: { value: "Lecture Hall" } });
    fireEvent.click(screen.getByText("Create system"));
    await waitFor(() => expect(captured).toBeTruthy());
    // Omitted is "generate one"; "" is a name of nothing the API refuses.
    expect("name" in captured!).toBe(false);
    // The NAME the locked field was showing goes back as the precondition.
    expect(captured!.expected_name).toBe("classroom");
    expect(captured!.label).toBe("Lecture Hall");
  });
});

// The label pen on the edit blade (#693). The "Generated" chip left the lists,
// and the fact landed beside the field an operator can act on, as the same lock
// the create form carries. This block proves the page is WIRED to the shared
// field (components/LabelPenField.test.tsx proves the field's own contract) and
// proves the half no component test can see: what a Save posts.
