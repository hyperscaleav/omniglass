import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor, fireEvent, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Locations from "./Locations";
import { LOCATIONS_KEY, type Location } from "../lib/locations";
import { LOCATION_TYPES_KEY, type LocationType } from "../lib/location_types";
import { ownerPropertiesKey, type EffectiveProperty } from "../lib/owner_properties";
import { ME_KEY, type Me } from "../lib/auth";
import { TAGS_KEY, entityTagsKey } from "../lib/tags";
import { uuidFor } from "../lib/testids";

// The Locations page on the shared TreeList in the create-as-route model: New routes
// to /locations/create (a draft accordion), Save hands off to /locations/<name> in
// edit; the detail is read-only in view (no in-body mutation control) and editable
// via the pencil. The detail also carries the Properties panel, which resolves the
// location type's declared-property contract against the location's own values.
// Data is seeded into the query cache so no server is needed; `>` grants every
// permission.
const me: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const hq: Location = { id: uuidFor("l-hq"), name: "hq", display_name: "HQ", location_type: "campus", effective_tags: {} };
const lab: Location = { id: uuidFor("l-lab"), name: "lab", display_name: "Lab", location_type: "campus", effective_tags: {} };
const hqB1: Location = { id: uuidFor("l-b1"), name: "hq-b1", display_name: "HQ B1", location_type: "building", parent: "hq", parent_id: hq.id, effective_tags: {} };
// Registry rows carry a uuid id and the name in name (ADR-0062); the
// server stores and compares the handle everywhere a location references its
// type, so a fixture with the handle in the id slot would hide a uuid-vs-name
// join bug (that is how #466 shipped).
const types: LocationType[] = [
  { id: uuidFor("lt-campus"), name: "campus", display_name: "Campus", icon: "landmark", official: true, allowed_parent_types: ["root"] },
  { id: uuidFor("lt-building"), name: "building", display_name: "Building", icon: "building", official: true, allowed_parent_types: ["root", "campus"] },
  // Unconstrained: any parent. Exists so the self-exclusion test below cannot
  // lean on the allowed-parents filter to hide the node's own subtree.
  { id: uuidFor("lt-area"), name: "area", display_name: "Area", icon: "map-pin", official: false, allowed_parent_types: [] },
];
// The campus type's contract, resolved against hq: one inherited default, plus one
// value hq sets that no contract declares.
const hqProperties: EffectiveProperty[] = [
  { property_type_name: "site.timezone", property_type_id: "site.timezone-id", display_name: "Time zone", data_type: "string", required: false, is_set: false, from_contract: true, default_value: "UTC", value: "UTC" },
  { property_type_name: "site.note", property_type_id: "site.note-id", display_name: "Note", data_type: "string", required: false, is_set: true, from_contract: false, set_value: "leased", value: "leased", value_id: "v-note" },
];

function mount(path: string, extraLocations: Location[] = []) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  const all = [hq, lab, hqB1, ...extraLocations];
  qc.setQueryData([...LOCATIONS_KEY], all);
  qc.setQueryData([...LOCATION_TYPES_KEY], types);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...TAGS_KEY], []);
  qc.setQueryData([...entityTagsKey("location", "hq")], []);
  qc.setQueryData([...entityTagsKey("location", "hq-b1")], []);
  for (const l of extraLocations) qc.setQueryData([...entityTagsKey("location", l.name)], []);
  // Seed every location's effective properties so the detail's panel resolves
  // from cache (the tests that fake fetch refuse any request they did not expect).
  for (const l of all) qc.setQueryData([...ownerPropertiesKey("location", l.name)], l.name === "hq" ? hqProperties : []);
  window.history.pushState({}, "", path);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="/locations" component={Locations} />
        <Route path="/locations/:name" component={Locations} />
      </Router>
    </QueryClientProvider>
  ));
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

  it("shows an existing location read-only in view: no tag add control, an Edit affordance", async () => {
    mount("/locations/hq");
    // The detail resolves and renders the read-only facts.
    await waitFor(() => expect(screen.getByText("Name")).toBeTruthy());
    // No in-body mutation control in view: the TagAdder add row is absent.
    expect(screen.queryByPlaceholderText(/Add a tag/)).toBeNull();
    // The view footer offers Edit (which would flip the accordion to edit mode).
    expect(screen.getByText("Edit")).toBeTruthy();
  });

  it("edit mode narrows the parent picker to the type's allowed_parent_types and excludes the node's own subtree", async () => {
    mount("/locations/hq-b1");
    await waitFor(() => expect(screen.getByText("Name")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    // building's allowed_parent_types is [root, campus]: both campuses (HQ, Lab)
    // are offered; hq-b1 itself never appears (self-exclusion); there is no
    // "Root (current)" option since hq-b1 already has a parent.
    const select = (await screen.findByLabelText("Parent")) as HTMLSelectElement;
    const optionLabels = Array.from(select.options).map((o) => o.textContent?.trim());
    expect(optionLabels).toContain("HQ");
    expect(optionLabels).toContain("Lab");
    expect(optionLabels).not.toContain("Root (current)");
    expect(optionLabels.some((l) => l?.includes("HQ B1"))).toBe(false);
  });

  it("offers only the current-root placeholder when the type's allowed set has no real matching location", async () => {
    mount("/locations/hq");
    await waitFor(() => expect(screen.getByText("Name")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    // campus's allowed_parent_types is [root]: no location is of type "root" (it
    // is not a real location_type), so the only option is the current-root
    // placeholder; hq has nowhere else it could move in this fixture.
    const select = (await screen.findByLabelText("Parent")) as HTMLSelectElement;
    const optionLabels = Array.from(select.options).map((o) => o.textContent?.trim());
    expect(optionLabels).toEqual(["Root (current)"]);
  });

  it("selecting a different parent updates the picker's value, seeded from the current parent", async () => {
    mount("/locations/hq-b1");
    await waitFor(() => expect(screen.getByText("Name")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    const select = (await screen.findByLabelText("Parent")) as HTMLSelectElement;
    // The picker is keyed and valued on uuid, not name (#627), so its value
    // seeds from the current parent's id, not "hq".
    expect(select.value).toBe(hq.id);
    fireEvent.change(select, { target: { value: lab.id } });
    expect(select.value).toBe(lab.id);
  });

  it("offers a real non-root parent for a currently-root location and sends the move on save", async () => {
    // b2 is a building sitting at root (no parent_id), same as hq-b1 started life
    // per the motivating scenario: an operator creates a building at root, later
    // adds a campus, then moves the building under it. building's allowed_parent_types
    // is [root, campus], so the real campus HQ must be offered as a candidate even
    // though b2 is currently root, not filtered out just because there is no current
    // parent to compare against.
    const b2: Location = { id: uuidFor("l-b2"), name: "b2", display_name: "B2", location_type: "building", effective_tags: {} };
    mount("/locations/b2", [b2]);
    await waitFor(() => expect(screen.getByText("Name")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    const select = (await screen.findByLabelText("Parent")) as HTMLSelectElement;
    const optionLabels = Array.from(select.options).map((o) => o.textContent?.trim());
    expect(optionLabels).toContain("HQ");
    expect(optionLabels).toContain("Root (current)");
    // The picker is keyed and valued on uuid, not name (#627).
    fireEvent.change(select, { target: { value: hq.id } });
    expect(select.value).toBe(hq.id);
    let captured: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const method = req.method;
      const url = req.url;
      if (method === "PATCH" && url.includes("/locations/b2")) {
        return new Response(JSON.stringify(b2), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      // The move is its own call, POST .../{name}:move, not a PATCH field
      // (#627): placement left the patch body entirely.
      if (method === "POST" && url.includes("/locations/b2:move")) {
        captured = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({ ...b2, parent: "hq" }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      throw new Error(`unexpected fetch in this test: ${method} ${url}`);
    });
    fireEvent.click(screen.getByText("Save changes"));
    await waitFor(() => expect(captured).toBeTruthy());
    // Posted as the uuid (the API dual-accepts uuid-or-name, ADR-0062), not
    // the name the picker used to send.
    expect((captured as { parent?: string }).parent).toBe(hq.id);
  });

  it("saving a rejected move surfaces the 422 through the existing inline alert and stays in edit mode", async () => {
    mount("/locations/hq-b1");
    await waitFor(() => expect(screen.getByText("Name")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    const select = (await screen.findByLabelText("Parent")) as HTMLSelectElement;
    fireEvent.change(select, { target: { value: "lab" } });
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const method = req.method;
      const url = req.url;
      if (method === "PATCH" && url.includes("/locations/hq-b1")) {
        return new Response(JSON.stringify(hqB1), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (method === "POST" && url.includes("/locations/hq-b1:move")) {
        return new Response(JSON.stringify({ detail: "building may not be placed under campus lab" }), { status: 422, headers: { "Content-Type": "application/json" } });
      }
      throw new Error(`unexpected fetch in this test: ${method} ${url}`);
    });
    fireEvent.click(screen.getByText("Save changes"));
    expect(await screen.findByText(/may not be placed under/)).toBeTruthy();
    // Still in edit mode: the picker (not the read-only fact) is still on screen.
    expect(screen.getByLabelText("Parent")).toBeTruthy();
  });

  it("excludes the node's own subtree, keyed and excluded by uuid (#466, #627)", async () => {
    // area is unconstrained, so the candidate pool is every location; only
    // subtree exclusion can keep area1 and its child out. The candidates are
    // now keyed by uuid, not name (#627: two locations can share a name), and
    // excludeSubtreeOf passes the location's uuid to match, so the exclusion
    // still keeps the node and its own subtree out.
    const area1: Location = { id: uuidFor("l-area1"), name: "area1", display_name: "Area 1", location_type: "area", effective_tags: {} };
    const area2: Location = { id: uuidFor("l-area2"), name: "area2", display_name: "Area 2", location_type: "area", parent: "area1", parent_id: area1.id, effective_tags: {} };
    mount("/locations/area1", [area1, area2]);
    await waitFor(() => expect(screen.getByText("Name")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    const select = (await screen.findByLabelText("Parent")) as HTMLSelectElement;
    const optionLabels = Array.from(select.options).map((o) => o.textContent?.trim());
    expect(optionLabels).toContain("HQ");
    expect(optionLabels.some((l) => l?.includes("Area 1"))).toBe(false);
    expect(optionLabels.some((l) => l?.includes("Area 2"))).toBe(false);
  });

  it("offers two same-named parent candidates as distinct, independently selectable options (#627)", async () => {
    // Two roots named "annex" (legal after #627): a name-keyed picker would
    // have collapsed them into one value-identical option, so choosing
    // "annex" could never say which one was meant, and posting it would name
    // an ambiguous ref the API refuses. Keyed by uuid, both render and each
    // is selectable on its own.
    const annexA: Location = { id: uuidFor("l-annex-a"), name: "annex", display_name: "Annex", location_type: "campus", effective_tags: {} };
    const annexB: Location = { id: uuidFor("l-annex-b"), name: "annex", display_name: "Annex", location_type: "campus", effective_tags: {} };
    mount("/locations/hq-b1", [annexA, annexB]);
    await waitFor(() => expect(screen.getByText("Name")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    const select = (await screen.findByLabelText("Parent")) as HTMLSelectElement;
    const values = Array.from(select.options).map((o) => o.value);
    expect(values).toContain(annexA.id);
    expect(values).toContain(annexB.id);
    fireEvent.change(select, { target: { value: annexB.id } });
    expect(select.value).toBe(annexB.id);
    fireEvent.change(select, { target: { value: annexA.id } });
    expect(select.value).toBe(annexA.id);
  });

  it("posts the location_type handle, never the uuid, on create (#466)", async () => {
    let captured: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST" && req.url.includes("/locations")) {
        captured = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({ id: uuidFor("l-annex"), name: "annex", location_type: "campus" }), { status: 201, headers: { "Content-Type": "application/json" } });
      }
      throw new Error(`unexpected fetch in this test: ${req.method} ${req.url}`);
    });
    mount("/locations/create");
    await waitFor(() => expect(screen.getByText("New location")).toBeTruthy());
    fireEvent.input(screen.getByPlaceholderText("Conf Room 301"), { target: { value: "Annex" } });
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

  it("a campus row wears its type's landmark glyph, not the unknown-type fallback (#466)", async () => {
    mount("/locations");
    await waitFor(() => expect(screen.getByText("HQ")).toBeTruthy());
    const row = screen.getByText("HQ").closest("tr")!;
    // The Landmark glyph's pediment path; MapPin (the unknown-type fallback)
    // draws a teardrop instead. The icon map joins the node's type (a name) to
    // the registry, so a uuid-keyed map degrades every row to the fallback.
    expect(row.querySelector('path[d="m12 2 9 5H3z"]')).toBeTruthy();
    expect(row.querySelector('path[d^="M20 10c0 6-8 12"]')).toBeNull();
  });

  it("edit mode exposes an editable name with a check button", async () => {
    mount("/locations/hq");
    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    // The name becomes an editable input seeded from the row.
    const nameInput = (await screen.findByDisplayValue("hq")) as HTMLInputElement;
    expect(nameInput.disabled).toBe(false);
    // An inline check button sits beside it.
    expect(screen.getByLabelText("Check name")).toBeTruthy();
  });

  it("a fresh detail view keeps the name read-only", async () => {
    mount("/locations/hq");
    await waitFor(() => expect(screen.getByText("Name")).toBeTruthy());
    // No check button until edit begins: the name is a read-only fact.
    expect(screen.queryByLabelText("Check name")).toBeNull();
  });
});

// The Properties panel on the location detail is the shared owner panel, pointed at
// the location arc: the location type's contract resolved against the location's own
// values, with anything the location sets that no contract declares grouped apart.
describe("Locations properties panel", () => {
  afterEach(() => window.history.pushState({}, "", "/"));

  it("resolves the location type's contract on the detail, off-contract values apart", async () => {
    mount("/locations/hq");
    await waitFor(() => expect(screen.getByText("Properties")).toBeTruthy());
    expect(screen.getByText("the location type contract, resolved")).toBeTruthy();
    expect(screen.getByText("Time zone")).toBeTruthy();
    expect(screen.getByText("UTC")).toBeTruthy();
    const offContract = screen.getByRole("group", { name: /off contract/i });
    expect(within(offContract).getByText("Note")).toBeTruthy();
    expect(screen.getByText("set on this location, not declared by its location type")).toBeTruthy();
  });

  it("says where a location's properties come from when nothing is declared or set", async () => {
    mount("/locations/lab");
    await waitFor(() => expect(screen.getByText("Properties")).toBeTruthy());
    expect(screen.getByText(/come from its location type/)).toBeTruthy();
  });

  it("stages an override and flushes it to the location's own property route on Save", async () => {
    const calls: { method: string; url: string; body: string }[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      let body = "";
      try { body = await req.clone().text(); } catch { body = ""; }
      calls.push({ method: req.method, url: req.url, body });
      if (req.method === "PUT") {
        return new Response(JSON.stringify({ location: "hq", property_type_name: "site.timezone", property_type_id: "site.timezone-id", value: "America/Denver", value_id: "v-1" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      return new Response(JSON.stringify({ ...hq, locations: [hq], properties: hqProperties }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });

    mount("/locations/hq");
    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));

    const cell = (screen.getByText("Time zone").closest("div") as HTMLElement).parentElement as HTMLElement;
    fireEvent.click(within(cell).getByRole("checkbox"));
    fireEvent.input(within(cell).getByRole("textbox"), { target: { value: "America/Denver" } });

    fireEvent.click(screen.getByText("Save changes"));

    await waitFor(() => {
      const put = calls.find((c) => c.method === "PUT");
      expect(put).toBeTruthy();
      expect(put!.url).toContain("/locations/hq/properties/site.timezone");
      expect(JSON.parse(put!.body)).toEqual({ value: "America/Denver" });
    });
  });
});

// #627 scopes name uniqueness to placement, not the whole estate: two
// locations under different parents may now legally share a name. The tree
// builder used to key its construction-time map on the bare name
// (byId.set(l.name, ...)), so the second same-named row silently overwrote
// the first and its children reparented onto the survivor. Keying that map
// on uuid instead (node.id itself stays the name; only the construction key
// moved) is what keeps both rows in the rendered tree.
describe("Locations list survives duplicate names across placements (#627)", () => {
  afterEach(() => window.history.pushState({}, "", "/"));

  it("renders both same-named locations when they sit under different parents, each keeping its own child", async () => {
    // Each "room-1" has its OWN child (desk-a / desk-b): a bare row count
    // could still look right off a double-push artifact (the surviving node
    // object gets pushed into both parents' children arrays). The
    // discriminating symptom the amendment actually describes is the CHILD
    // reparenting onto whichever same-named node won the map: under the old
    // bug, both desks end up merged onto one surviving "room-1" object and
    // so both appear TWICE; under the fix, each desk renders exactly once,
    // under its own parent.
    const bldgA: Location = { id: uuidFor("l-bldg-a"), name: "bldg-a", location_type: "building", effective_tags: {} };
    const bldgB: Location = { id: uuidFor("l-bldg-b"), name: "bldg-b", location_type: "building", effective_tags: {} };
    const roomInA: Location = { id: uuidFor("l-room-a"), name: "room-1", location_type: "room", parent: "bldg-a", parent_id: bldgA.id, effective_tags: {} };
    const roomInB: Location = { id: uuidFor("l-room-b"), name: "room-1", location_type: "room", parent: "bldg-b", parent_id: bldgB.id, effective_tags: {} };
    const deskA: Location = { id: uuidFor("l-desk-a"), name: "desk-a", location_type: "area", parent: "room-1", parent_id: roomInA.id, effective_tags: {} };
    const deskB: Location = { id: uuidFor("l-desk-b"), name: "desk-b", location_type: "area", parent: "room-1", parent_id: roomInB.id, effective_tags: {} };

    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...LOCATIONS_KEY], [bldgA, bldgB, roomInA, roomInB, deskA, deskB]);
    qc.setQueryData([...LOCATION_TYPES_KEY], types);
    qc.setQueryData([...ME_KEY], me);
    qc.setQueryData([...TAGS_KEY], []);
    window.history.pushState({}, "", "/locations");
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="/locations" component={Locations} />
        </Router>
      </QueryClientProvider>
    ));

    await waitFor(() => expect(screen.getAllByText("bldg-a").length).toBeGreaterThan(0));
    // Tree mode starts fully collapsed, so expand everything.
    fireEvent.click(screen.getByTitle("Expand all"));
    // Rows are matched by their NAME cell specifically (the first <td>), not
    // by a bare text search: the "Parent" column also prints a row's parent
    // NAME as plain text, and desk-a/desk-b's parent is "room-1" too, which
    // would otherwise double-count as a false match.
    const rows = () => Array.from(document.querySelectorAll("tbody tr"));
    const nameCell = (row: Element) => row.querySelector("td")?.textContent ?? "";
    const rowsNamed = (name: string) => rows().filter((r) => nameCell(r).includes(name));
    await waitFor(() => expect(rowsNamed("room-1")).toHaveLength(2));
    expect(rowsNamed("desk-a")).toHaveLength(1);
    expect(rowsNamed("desk-b")).toHaveLength(1);
    // desk-a sits under the SAME "room-1" row as bldg-a, desk-b under
    // bldg-b's: the tree renders depth-first, so desk-a's row falls
    // strictly between bldg-a's and bldg-b's, and desk-b's falls after
    // bldg-b's.
    const indexOf = (name: string) => rows().indexOf(rowsNamed(name)[0]);
    expect(indexOf("desk-a")).toBeGreaterThan(indexOf("bldg-a"));
    expect(indexOf("desk-a")).toBeLessThan(indexOf("bldg-b"));
    expect(indexOf("desk-b")).toBeGreaterThan(indexOf("bldg-b"));
  });
});

// The list row carries BOTH identities: the display name an operator reads, and
// the key the API and CLI address the row by. The key is what somebody types into
// `omniglass location get <key>`, so it is on the row rather than behind a hover:
// hover does not exist on touch, is not discoverable, and cannot be selected to
// copy.
//
// Before this the row showed one or the other and never both, and the rule that
// picked between them was written out six times across the console.
describe("Locations list identity", () => {
  afterEach(() => window.history.pushState({}, "", "/"));

  it("shows the display name with the key beneath it", async () => {
    mount("/locations");
    await waitFor(() => expect(screen.getByText("HQ")).toBeTruthy());
    // Both, on the same row, not one standing in for the other.
    const row = screen.getByText("HQ").closest("tr")!;
    expect(within(row).getByText("hq")).toBeTruthy();
  });

  it("shows the key once when the entity has no display name", async () => {
    const bare: Location = { id: uuidFor("l-bare"), name: "hq-boardroom-nvx-tx", location_type: "campus", effective_tags: {} };
    mount("/locations", [bare]);
    await waitFor(() => expect(screen.getByText("hq-boardroom-nvx-tx")).toBeTruthy());
    // Rendered once, not duplicated as label-plus-key: the label IS the key, and
    // nothing is derived from it (a sentence-cased "Hq boardroom nvx tx" would
    // read as a typo and mangle every acronym in the domain).
    expect(screen.getAllByText("hq-boardroom-nvx-tx")).toHaveLength(1);
  });
});

// The create form leads with the display name and derives the key from it, so an
// operator types "Conf Room 301" and never has to invent `conf-room-301` or think
// about the character class the API enforces.
//
// What each tier can actually witness, which is worth being precise about:
//
//   - THIS file proves the page is WIRED to the primitive. Remove the derivation
//     and both tests below fail. A unit test on the hook cannot tell you a page
//     forgot to use it.
//   - lib/entities.test.ts proves the SUPPRESSION: that a hand-edited key stops
//     following. That cannot be asserted from here. Once a user types into an
//     input, its DOM value property no longer tracks the signal, so `key.value`
//     returns what the test typed and would pass even with the rule removed. The
//     hint tracks ownership rather than suppression, so it cannot witness it
//     either. Mutation-checked in both files.
describe("Locations create identity", () => {
  afterEach(() => window.history.pushState({}, "", "/"));

  const fields = async () => {
    mount("/locations/create");
    await waitFor(() => expect(screen.getByText("New location")).toBeTruthy());
    const display = screen.getByPlaceholderText("Conf Room 301") as HTMLInputElement;
    const key = screen.getByPlaceholderText("hq-a-301") as HTMLInputElement;
    return { display, key };
  };

  it("derives the key as the display name is typed", async () => {
    const { display, key } = await fields();
    fireEvent.input(display, { target: { value: "Conf Room 301" } });
    await waitFor(() => expect(key.value).toBe("conf-room-301"));
  });

  it("stops advertising the key as derived once it is edited by hand", async () => {
    const { display, key } = await fields();
    fireEvent.input(display, { target: { value: "Conf Room 301" } });
    await waitFor(() => expect(key.value).toBe("conf-room-301"));

    fireEvent.input(key, { target: { value: "hq-a-301" } });
    fireEvent.input(display, { target: { value: "Conference Room 301 East" } });

    // The observable this CAN assert: the field stops advertising itself as
    // derived once the operator takes it, which is what they see.
    await waitFor(() => expect(display.value).toBe("Conference Room 301 East"));
    expect(screen.getByText(/Globally unique address/)).toBeTruthy();
    expect(screen.queryByText(/Derived from the display name/)).toBeNull();
  });
});
