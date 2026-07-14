import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor, fireEvent } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Locations from "./Locations";
import { LOCATIONS_KEY, LOCATION_TYPES_KEY, type Location, type LocationType } from "../lib/locations";
import { ME_KEY, type Me } from "../lib/auth";
import { TAGS_KEY, entityTagsKey } from "../lib/tags";

// The Locations page on the shared TreeList in the create-as-route model: New routes
// to /locations/create (a draft accordion), Save hands off to /locations/<name> in
// edit; the detail is read-only in view (no in-body mutation control) and editable
// via the pencil. Data is seeded into the query cache so no server is needed; `>`
// grants every permission.
const me: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const hq: Location = { id: "l-hq", name: "hq", display_name: "HQ", location_type: "campus", effective_tags: {} };
const lab: Location = { id: "l-lab", name: "lab", display_name: "Lab", location_type: "campus", effective_tags: {} };
const hqB1: Location = { id: "l-b1", name: "hq-b1", display_name: "HQ B1", location_type: "building", parent_id: "l-hq", effective_tags: {} };
const types: LocationType[] = [
  { id: "campus", display_name: "Campus", icon: "landmark", official: true, allowed_parent_types: ["root"] },
  { id: "building", display_name: "Building", icon: "building", official: true, allowed_parent_types: ["root", "campus"] },
];

function mount(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...LOCATIONS_KEY], [hq, lab, hqB1]);
  qc.setQueryData([...LOCATION_TYPES_KEY], types);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...TAGS_KEY], []);
  qc.setQueryData([...entityTagsKey("location", "hq")], []);
  qc.setQueryData([...entityTagsKey("location", "hq-b1")], []);
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
});
