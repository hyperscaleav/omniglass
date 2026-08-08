import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor, fireEvent, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Systems from "./Systems";
import { SYSTEMS_KEY, type System } from "../lib/systems";
import { LOCATIONS_KEY } from "../lib/locations";
import { COMPONENTS_KEY } from "../lib/components";
import { STANDARDS_KEY, type Standard } from "../lib/standards";
import { ownerPropertiesKey, type EffectiveProperty } from "../lib/owner_properties";
import { ME_KEY, type Me } from "../lib/auth";
import { TAGS_KEY, entityTagsKey } from "../lib/tags";
import { uuidFor } from "../lib/testids";
import { hueFor } from "../lib/system_color";

// The Systems page on the shared TreeList in the create-as-route model: New routes
// to /systems/create (a draft accordion), Save hands off to /systems/<id> in edit;
// the detail is read-only in view (no in-body mutation control) and editable via the
// pencil. A system conforms to a STANDARD, whose declared-property contract the
// detail's Properties panel resolves. Data is seeded into the query cache so no
// server is needed; `>` grants every permission.
const me: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const sys: System = { id: uuidFor("s-1"), name: "boardroom", display_name: "Boardroom", standard: "meeting-room", standard_id: uuidFor("std-meeting-room"), member_count: 2, effective_tags: {} };
const standards: Standard[] = [
  { id: uuidFor("meeting-room"), name: "meeting-room", display_name: "Meeting room", official: true },
  { id: uuidFor("huddle-space"), name: "huddle-space", display_name: "Huddle space", official: false },
];
// The standard's contract, resolved against the system: one inherited default and
// one value the system sets directly with nothing declaring it.
const properties: EffectiveProperty[] = [
  { property_type_name: "seat_count", property_type_id: "seat_count-id", display_name: "Seat count", data_type: "int", required: false, is_set: false, from_contract: true, default_value: 12, value: 12 },
  { property_type_name: "room.note", property_type_id: "room.note-id", display_name: "Note", data_type: "string", required: false, is_set: true, from_contract: false, set_value: "corner room", value: "corner room", value_id: "v-note" },
];

function mount(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...SYSTEMS_KEY], [sys]);
  qc.setQueryData([...LOCATIONS_KEY], []);
  qc.setQueryData([...COMPONENTS_KEY], []);
  qc.setQueryData([...STANDARDS_KEY], standards);
  qc.setQueryData([...ownerPropertiesKey("system", "boardroom")], properties);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...TAGS_KEY], []);
  qc.setQueryData([...entityTagsKey("system", "boardroom")], []);
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

  it("wears a colour dot derived from its uuid on the list row", async () => {
    mount("/systems");
    await waitFor(() => expect(screen.getByText("Boardroom")).toBeTruthy());
    const dot = document.querySelector(".og-system-dot") as HTMLElement;
    expect(dot).toBeTruthy();
    expect(dot.style.getPropertyValue("--sys-h")).toBe(String(hueFor(sys.id)));
  });

  it("shows an existing system read-only in view: no tag add control, an Edit affordance", async () => {
    mount("/systems/boardroom");
    // The detail resolves and renders the read-only facts.
    await waitFor(() => expect(screen.getByText("Name")).toBeTruthy());
    // No in-body mutation control in view: the TagAdder add row is absent.
    expect(screen.queryByPlaceholderText(/Add a tag/)).toBeNull();
    // The view footer offers Edit (which would flip the accordion to edit mode).
    expect(screen.getByText("Edit")).toBeTruthy();
  });

  it("edit mode exposes an editable name with a check button", async () => {
    mount("/systems/boardroom");
    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    // The name becomes an editable input seeded from the row.
    const nameInput = (await screen.findByDisplayValue("boardroom")) as HTMLInputElement;
    expect(nameInput.disabled).toBe(false);
    // An inline check button sits beside it.
    expect(screen.getByLabelText("Check name")).toBeTruthy();
  });

  it("a fresh detail view keeps the name read-only", async () => {
    mount("/systems/boardroom");
    await waitFor(() => expect(screen.getByText("Name")).toBeTruthy());
    // No check button until edit begins: the name is a read-only fact.
    expect(screen.queryByLabelText("Check name")).toBeNull();
  });

  it("shows the system's standard by display name, not its id", async () => {
    mount("/systems/boardroom");
    await waitFor(() => expect(screen.getByText("Name")).toBeTruthy());
    expect(screen.getAllByText("Meeting room").length).toBeGreaterThan(0);
  });

  // #627 Task 15c: routes take :id now. A name-shaped deep link resolves
  // through TreeList's byAddr fallback and corrects the address bar to the
  // uuid (replace); a rename afterward must leave that URL alone, since the
  // id it addresses never changes.
  it("redirects a name-shaped deep link to the resolved uuid, and a rename leaves the route where it is", async () => {
    mount("/systems/boardroom");
    await waitFor(() => expect(window.location.pathname).toBe(`/systems/${sys.id}`));
    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    const nameInput = (await screen.findByDisplayValue("boardroom")) as HTMLInputElement;
    fireEvent.input(nameInput, { target: { value: "exec-boardroom" } });
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const { method, url } = req;
      if (method === "PATCH" && url.includes("/systems/boardroom")) {
        return new Response(JSON.stringify(sys), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (method === "POST" && url.includes("/systems/boardroom:rename")) {
        return new Response(JSON.stringify({ ...sys, name: "exec-boardroom" }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (method === "GET") {
        return new Response(JSON.stringify({ systems: [{ ...sys, name: "exec-boardroom" }] }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      throw new Error(`unexpected fetch in this test: ${method} ${url}`);
    });
    fireEvent.click(screen.getByText("Save changes"));
    await waitFor(() => expect(screen.queryByLabelText("Check name")).toBeNull());
    expect(window.location.pathname).toBe(`/systems/${sys.id}`);
  });

  it("renders an explicit not-found state for an address that matches no system, not the old silent list fallback", async () => {
    mount("/systems/no-such-system");
    expect(await screen.findByText(/No such system/)).toBeTruthy();
    expect(screen.getByText(/old address/)).toBeTruthy();
    expect(screen.queryByText("Boardroom")).toBeNull();
  });
});

// #627 scopes name uniqueness to placement, not the whole estate: two systems
// under different parents may now legally share a name. The tree builder
// used to key its construction-time map on the bare name (byId.set(s.name,
// ...)), so the second same-named row silently overwrote the first and its
// children reparented onto the survivor. Keying that map on uuid instead
// (node.id itself stays the name; only the construction key moved) is what
// keeps both rows in the rendered tree.
describe("Systems list survives duplicate names across placements (#627)", () => {
  afterEach(() => window.history.pushState({}, "", "/"));

  it("renders both same-named systems when they sit under different parents, each keeping its own child", async () => {
    // Each "edge" has its OWN child (leaf-av / leaf-lab): a bare row count
    // could still look right off a double-push artifact (the surviving node
    // object gets pushed into both parents' children arrays). The
    // discriminating symptom the amendment actually describes is the CHILD
    // reparenting onto whichever same-named node won the map: under the old
    // bug, both leaves end up merged onto one surviving "edge" object and so
    // both appear TWICE; under the fix, each leaf renders exactly once,
    // under its own parent.
    const av: System = { id: uuidFor("s-av"), name: "av", member_count: 0, effective_tags: {} };
    const lab: System = { id: uuidFor("s-lab"), name: "lab", member_count: 0, effective_tags: {} };
    const edgeUnderAV: System = { id: uuidFor("s-edge-av"), name: "edge", parent: "av", parent_id: av.id, member_count: 0, effective_tags: {} };
    const edgeUnderLab: System = { id: uuidFor("s-edge-lab"), name: "edge", parent: "lab", parent_id: lab.id, member_count: 0, effective_tags: {} };
    const leafAV: System = { id: uuidFor("s-leaf-av"), name: "leaf-av", parent: "edge", parent_id: edgeUnderAV.id, member_count: 0, effective_tags: {} };
    const leafLab: System = { id: uuidFor("s-leaf-lab"), name: "leaf-lab", parent: "edge", parent_id: edgeUnderLab.id, member_count: 0, effective_tags: {} };

    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...SYSTEMS_KEY], [av, lab, edgeUnderAV, edgeUnderLab, leafAV, leafLab]);
    qc.setQueryData([...LOCATIONS_KEY], []);
    qc.setQueryData([...COMPONENTS_KEY], []);
    qc.setQueryData([...STANDARDS_KEY], standards);
    qc.setQueryData([...ME_KEY], me);
    qc.setQueryData([...TAGS_KEY], []);
    window.history.pushState({}, "", "/systems");
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="/systems" component={Systems} />
        </Router>
      </QueryClientProvider>
    ));

    await waitFor(() => expect(screen.getAllByText("av").length).toBeGreaterThan(0));
    // Tree mode starts fully collapsed, so expand everything.
    fireEvent.click(screen.getByTitle("Expand all"));
    await waitFor(() => expect(screen.getAllByText("edge")).toHaveLength(2));
    expect(screen.getAllByText("leaf-av")).toHaveLength(1);
    expect(screen.getAllByText("leaf-lab")).toHaveLength(1);
    // leaf-av sits under the SAME "edge" row as av, leaf-lab under lab's: the
    // tree renders depth-first, so leaf-av's row falls strictly between av's
    // and lab's, and leaf-lab's falls after lab's.
    const rows = Array.from(document.querySelectorAll("tbody tr"));
    const indexOf = (text: string) => rows.indexOf(screen.getByText(text).closest("tr")!);
    expect(indexOf("leaf-av")).toBeGreaterThan(indexOf("av"));
    expect(indexOf("leaf-av")).toBeLessThan(indexOf("lab"));
    expect(indexOf("leaf-lab")).toBeGreaterThan(indexOf("lab"));
  });
});

// The Properties panel on the system detail is the shared owner panel, pointed at
// the system arc: the standard's contract resolved against the system's own values,
// with anything the system sets that no contract declares grouped off contract.
describe("Systems properties panel", () => {
  afterEach(() => window.history.pushState({}, "", "/"));

  it("resolves the standard's contract on the detail, off-contract values apart", async () => {
    mount("/systems/boardroom");
    await waitFor(() => expect(screen.getByText("Properties")).toBeTruthy());
    expect(screen.getByText("the standard contract, resolved")).toBeTruthy();
    // The inherited contract default reads muted, with no override dot.
    expect(screen.getByText("Seat count")).toBeTruthy();
    expect(screen.getByText("12")).toBeTruthy();
    // What the system says about itself sits in its own group.
    const offContract = screen.getByRole("group", { name: /off contract/i });
    expect(within(offContract).getByText("Note")).toBeTruthy();
    expect(screen.getByText("set on this system, not declared by its standard")).toBeTruthy();
  });

  it("stages an override and flushes it to the system's own property route on Save", async () => {
    const calls: { method: string; url: string; body: string }[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      let body = "";
      try { body = await req.clone().text(); } catch { body = ""; }
      calls.push({ method: req.method, url: req.url, body });
      if (req.method === "PATCH") {
        return new Response(JSON.stringify(sys), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (req.method === "PUT") {
        return new Response(JSON.stringify({ system: "boardroom", property_type_name: "seat_count", property_type_id: "seat_count-id", value: 8, value_id: "v-1" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      return new Response(JSON.stringify({ systems: [sys], properties, standards, locations: [], components: [] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });

    mount("/systems/boardroom");
    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));

    // Override the inherited contract default with a value of this system's own.
    const cell = (screen.getByText("Seat count").closest("div") as HTMLElement).parentElement as HTMLElement;
    fireEvent.click(within(cell).getByRole("checkbox"));
    fireEvent.input(within(cell).getByRole("spinbutton"), { target: { value: "8" } });

    // The panel batches into the accordion's Save, alongside the system's core facts.
    fireEvent.click(screen.getByText("Save changes"));

    await waitFor(() => {
      const put = calls.find((c) => c.method === "PUT");
      expect(put).toBeTruthy();
      expect(put!.url).toContain("/systems/boardroom/properties/seat_count");
      expect(JSON.parse(put!.body)).toEqual({ value: 8 }); // coerced to its data_type
    });
  });
});
