import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor, fireEvent, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Locations from "./Locations";
import { LOCATIONS_KEY, LOCATION_TYPES_KEY, type Location, type LocationType } from "../lib/locations";
import { ownerPropertiesKey, type EffectiveProperty } from "../lib/owner_properties";
import { ME_KEY, type Me } from "../lib/auth";
import { TAGS_KEY, entityTagsKey } from "../lib/tags";

// The Locations page on the shared TreeList in the create-as-route model: New routes
// to /locations/create (a draft accordion), Save hands off to /locations/<name> in
// edit; the detail is read-only in view (no in-body mutation control) and editable
// via the pencil. The detail also carries the Properties panel, which resolves the
// location type's declared-property contract against the location's own values.
// Data is seeded into the query cache so no server is needed; `>` grants every
// permission.
const me: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const hq: Location = { id: "l-hq", name: "hq", display_name: "HQ", location_type: "campus", effective_tags: {} };
const lab: Location = { id: "l-lab", name: "lab", display_name: "Lab", location_type: "campus", effective_tags: {} };
const hqB1: Location = { id: "l-b1", name: "hq-b1", display_name: "HQ B1", location_type: "building", parent: "hq", effective_tags: {} };
const types: LocationType[] = [
  { id: "campus", display_name: "Campus", icon: "landmark", official: true, allowed_parent_types: ["root"] },
  { id: "building", display_name: "Building", icon: "building", official: true, allowed_parent_types: ["root", "campus"] },
];
// The campus type's contract, resolved against hq: one inherited default, plus one
// value hq sets that no contract declares.
const hqProperties: EffectiveProperty[] = [
  { property_name: "site.timezone", display_name: "Time zone", data_type: "string", required: false, is_set: false, from_contract: true, default_value: "UTC", value: "UTC" },
  { property_name: "site.note", display_name: "Note", data_type: "string", required: false, is_set: true, from_contract: false, set_value: "leased", value: "leased", value_id: "v-note" },
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
    await waitFor(() => expect(screen.getByText("Technical name")).toBeTruthy());
    // No in-body mutation control in view: the TagAdder add row is absent.
    expect(screen.queryByPlaceholderText(/Add a tag/)).toBeNull();
    // The view footer offers Edit (which would flip the accordion to edit mode).
    expect(screen.getByText("Edit")).toBeTruthy();
  });

  it("edit mode narrows the parent picker to the type's allowed_parent_types and excludes the node's own subtree", async () => {
    mount("/locations/hq-b1");
    await waitFor(() => expect(screen.getByText("Technical name")).toBeTruthy());
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
    await waitFor(() => expect(screen.getByText("Technical name")).toBeTruthy());
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
    await waitFor(() => expect(screen.getByText("Technical name")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    const select = (await screen.findByLabelText("Parent")) as HTMLSelectElement;
    expect(select.value).toBe("hq");
    fireEvent.change(select, { target: { value: "lab" } });
    expect(select.value).toBe("lab");
  });

  it("offers a real non-root parent for a currently-root location and sends the move on save", async () => {
    // b2 is a building sitting at root (no parent_id), same as hq-b1 started life
    // per the motivating scenario: an operator creates a building at root, later
    // adds a campus, then moves the building under it. building's allowed_parent_types
    // is [root, campus], so the real campus HQ must be offered as a candidate even
    // though b2 is currently root, not filtered out just because there is no current
    // parent to compare against.
    const b2: Location = { id: "l-b2", name: "b2", display_name: "B2", location_type: "building", effective_tags: {} };
    mount("/locations/b2", [b2]);
    await waitFor(() => expect(screen.getByText("Technical name")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    const select = (await screen.findByLabelText("Parent")) as HTMLSelectElement;
    const optionLabels = Array.from(select.options).map((o) => o.textContent?.trim());
    expect(optionLabels).toContain("HQ");
    expect(optionLabels).toContain("Root (current)");
    fireEvent.change(select, { target: { value: "hq" } });
    expect(select.value).toBe("hq");
    let captured: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const method = req.method;
      const url = req.url;
      if (method === "PATCH" && url.includes("/locations/b2")) {
        captured = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({ ...b2, parent: "hq" }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      throw new Error(`unexpected fetch in this test: ${method} ${url}`);
    });
    fireEvent.click(screen.getByText("Save changes"));
    await waitFor(() => expect(captured).toBeTruthy());
    expect((captured as { parent?: string }).parent).toBe("hq");
  });

  it("saving a rejected move surfaces the 422 through the existing inline alert and stays in edit mode", async () => {
    mount("/locations/hq-b1");
    await waitFor(() => expect(screen.getByText("Technical name")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    const select = (await screen.findByLabelText("Parent")) as HTMLSelectElement;
    fireEvent.change(select, { target: { value: "lab" } });
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const method = req.method;
      const url = req.url;
      if (method === "PATCH" && url.includes("/locations/hq-b1")) {
        return new Response(JSON.stringify({ detail: "building may not be placed under campus lab" }), { status: 422, headers: { "Content-Type": "application/json" } });
      }
      throw new Error(`unexpected fetch in this test: ${method} ${url}`);
    });
    fireEvent.click(screen.getByText("Save changes"));
    expect(await screen.findByText(/may not be placed under/)).toBeTruthy();
    // Still in edit mode: the picker (not the read-only fact) is still on screen.
    expect(screen.getByLabelText("Parent")).toBeTruthy();
  });

  it("edit mode exposes an editable technical name with a check button", async () => {
    mount("/locations/hq");
    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    // The technical name becomes an editable input seeded from the row.
    const nameInput = (await screen.findByDisplayValue("hq")) as HTMLInputElement;
    expect(nameInput.disabled).toBe(false);
    // An inline check button sits beside it.
    expect(screen.getByLabelText("Check name")).toBeTruthy();
  });

  it("a fresh detail view keeps the technical name read-only", async () => {
    mount("/locations/hq");
    await waitFor(() => expect(screen.getByText("Technical name")).toBeTruthy());
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
        return new Response(JSON.stringify({ location: "hq", property_name: "site.timezone", value: "America/Denver", value_id: "v-1" }), {
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
    const bare: Location = { id: "l-bare", name: "hq-boardroom-nvx-tx", location_type: "campus", effective_tags: {} };
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
