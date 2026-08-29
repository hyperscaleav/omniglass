import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import EntityForm, { EntityCreateForm } from "./EntityForm";
import { BladesContext, createBladeController, createEditSlot, type BladeEdit } from "../lib/blades";
import { SYSTEMS_KEY } from "../lib/systems";
import { LOCATIONS_KEY } from "../lib/locations";
import { LOCATION_TYPES_KEY } from "../lib/location_types";
import { STANDARDS_KEY } from "../lib/standards";
import { PRODUCTS_KEY } from "../lib/products";
import { SYSTEM_TYPES_KEY } from "../lib/system_types";
import { TAGS_KEY } from "../lib/tags";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// EntityForm (#826 slice 1): the ONE form per fleet kind. It renders read or
// edit against a slot its host owns (the workspace's Configure tab, the
// blade, the explorer's glance), so the fields and their gates are the same
// wherever the operator meets them. These tests drive it on a bare page host.

const calls: string[] = [];
vi.mock("../lib/locations", async (orig) => {
  const real = await orig<typeof import("../lib/locations")>();
  return {
    ...real,
    updateLocation: vi.fn(async (id: string) => { calls.push(`update:${id}`); return {}; }),
    moveLocation: vi.fn(async (id: string, parent: string) => { calls.push(`move:${id}:${parent}`); return {}; }),
    renameLocation: vi.fn(async (id: string, name: string) => { calls.push(`rename:${id}:${name}`); return {}; }),
  };
});

const owner: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const updaterOnly: Me = { principal: { id: "u-up", kind: "human" }, human: { username: "up" }, permissions: ["system:read", "system:update", "location:read", "location:update", "tag:read"], grants: [] };

function mount(kind: "system" | "location", id: string, me: Me = owner) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...SYSTEMS_KEY], [
    { id: uuidFor("ef-sys"), name: "huddle", label: "Huddle Room", standard: "huddle-room", system_type: "huddle", location: uuidFor("ef-room"), actions: ["update", "rename"] },
  ]);
  qc.setQueryData([...LOCATIONS_KEY], [
    { id: uuidFor("ef-hq"), name: "hq", label: "Headquarters", location_type: "campus", parent_id: null, actions: ["update", "move", "rename"] },
    { id: uuidFor("ef-west"), name: "west", label: "West Building", location_type: "building", parent_id: uuidFor("ef-hq"), actions: ["update", "move", "rename"] },
    { id: uuidFor("ef-room"), name: "huddle-room", label: "Huddle Room", location_type: "room", parent_id: uuidFor("ef-west"), actions: ["update", "move", "rename"] },
  ]);
  qc.setQueryData([...LOCATION_TYPES_KEY], [
    { id: uuidFor("t-campus"), name: "campus", label: "Campus", allowed_parent_types: ["root"] },
    { id: uuidFor("t-building"), name: "building", label: "Building", allowed_parent_types: ["root", "campus"] },
    { id: uuidFor("t-room"), name: "room", label: "Room", allowed_parent_types: ["building", "campus"] },
  ]);
  qc.setQueryData([...STANDARDS_KEY], [{ id: uuidFor("std"), name: "huddle-room", label: "Huddle Room" }]);
  qc.setQueryData([...SYSTEM_TYPES_KEY], [{ id: uuidFor("st"), name: "huddle", label: "Huddle" }]);
  qc.setQueryData([...TAGS_KEY], []);
  qc.setQueryData(["system-properties", uuidFor("ef-sys")], []);
  qc.setQueryData(["location-properties", uuidFor("ef-room")], []);
  window.history.pushState({}, "", "/web/x");
  let slot!: BladeEdit;
  render(() => (
    <QueryClientProvider client={qc}>
      <Router base="/web">
        <Route
          path="/x"
          component={() => {
            const blades = createBladeController();
            slot = createEditSlot();
            return (
              <BladesContext.Provider value={blades}>
                <EntityForm kind={kind} id={id} slot={slot} host="page" />
              </BladesContext.Provider>
            );
          }}
        />
      </Router>
    </QueryClientProvider>
  ));
  return { slot: () => slot };
}

afterEach(() => { cleanup(); calls.length = 0; });

describe("the one form, read", () => {
  it("renders the sections from the row in order: identity, classification, placement, tags", async () => {
    mount("system", uuidFor("ef-sys"));
    const form = await screen.findByTestId("entity-form");
    const eyebrows = within(form).getAllByText(/^(Identity|Classification|Placement|Tags)$/).map((e) => e.textContent);
    expect(eyebrows).toEqual(["Identity", "Classification", "Placement", "Tags"]);
    expect(within(form).getAllByText("huddle").length).toBeGreaterThan(0);
    expect(within(form).getByText("huddle-room")).toBeTruthy();
    // Placement reads the location's label, resolved by id, so the label
    // shows twice: once as the system's own label, once as where it sits.
    expect(within(form).getAllByText("Huddle Room").length).toBe(2);
    expect(within(form).getByText("Where it sits")).toBeTruthy();
  });
});

describe("the one form, editing", () => {
  it("begins on the host's slot and turns the fields into controls, the rename gate included", async () => {
    const { slot } = mount("system", uuidFor("ef-sys"));
    const form = await screen.findByTestId("entity-form");
    expect(slot().editable()).toBe(true);
    slot().begin();
    await waitFor(() => expect(within(form).getByRole("combobox", { name: /standard/i })).toBeTruthy());
    // The owner holds rename, so the name is an input beside its precheck.
    expect(within(form).getByRole("button", { name: /check/i })).toBeTruthy();
  });

  it("keeps the name read-only for a caller holding update but not rename", async () => {
    const { slot } = mount("system", uuidFor("ef-sys"), updaterOnly);
    const form = await screen.findByTestId("entity-form");
    expect(slot().editable()).toBe(true);
    slot().begin();
    await waitFor(() => expect(within(form).getByRole("combobox", { name: /standard/i })).toBeTruthy());
    expect(within(form).queryByRole("button", { name: /check/i })).toBeNull();
    expect(within(form).queryByDisplayValue("huddle")).toBeNull();
    expect(within(form).getByText(/Renaming needs the rename permission/)).toBeTruthy();
  });

  it("saves in the ruled order, update then move then rename, all addressed by uuid", async () => {
    const { slot } = mount("location", uuidFor("ef-room"));
    const form = await screen.findByTestId("entity-form");
    slot().begin();
    const parent = await within(form).findByRole("combobox", { name: /parent/i });
    fireEvent.change(parent, { target: { value: uuidFor("ef-hq") } });
    const name = within(form).getByDisplayValue("huddle-room");
    fireEvent.input(name, { target: { value: "huddle-2" } });
    await slot().save();
    expect(calls).toEqual([
      `update:${uuidFor("ef-room")}`,
      `move:${uuidFor("ef-room")}:${uuidFor("ef-hq")}`,
      `rename:${uuidFor("ef-room")}:huddle-2`,
    ]);
    expect(slot().editing()).toBe(false);
  });
});

describe("the one form, empty", () => {
  function mountCreate(kind: "system" | "location" | "component", under?: string) {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...ME_KEY], owner);
    qc.setQueryData([...SYSTEMS_KEY], []);
    qc.setQueryData([...LOCATIONS_KEY], [
      { id: uuidFor("ef-hq"), name: "hq", label: "Headquarters", location_type: "campus", parent_id: null, actions: ["update"] },
      { id: uuidFor("ef-west"), name: "west", label: "West Building", location_type: "building", parent_id: uuidFor("ef-hq"), actions: ["update"] },
    ]);
    qc.setQueryData([...LOCATION_TYPES_KEY], [{ id: uuidFor("t-room"), name: "room", label: "Room", allowed_parent_types: ["building"] }]);
    qc.setQueryData([...STANDARDS_KEY], []);
    qc.setQueryData([...SYSTEM_TYPES_KEY], []);
    qc.setQueryData([...PRODUCTS_KEY], []);
    window.history.pushState({}, "", "/web/x");
    render(() => (
      <QueryClientProvider client={qc}>
        <Router base="/web">
          <Route path="/x" component={() => <EntityCreateForm kind={kind} under={under} onCreated={() => {}} onCancel={() => {}} />} />
        </Router>
      </QueryClientProvider>
    ));
  }

  it("renders what and where before identity, and the create verb named for the kind", async () => {
    mountCreate("system");
    const form = await screen.findByTestId("entity-create-form");
    const eyebrows = within(form).getAllByText(/^(Classification|Placement|Identity|Tags)$/).map((e) => e.textContent);
    expect(eyebrows).toEqual(["Classification", "Placement", "Identity", "Tags"]);
    expect(within(form).getByRole("button", { name: /create system/i })).toBeTruthy();
  });

  it("prefills the placement from `under`, the explorer's create-where-you-stand", async () => {
    mountCreate("system", uuidFor("ef-west"));
    const form = await screen.findByTestId("entity-create-form");
    const location = within(form).getByLabelText("Location") as HTMLSelectElement;
    await waitFor(() => expect(location.value).toBe(uuidFor("ef-west")));
  });

  it("prefills a location's parent from `under`", async () => {
    mountCreate("location", uuidFor("ef-west"));
    const form = await screen.findByTestId("entity-create-form");
    const parent = within(form).getByLabelText("Parent") as HTMLSelectElement;
    await waitFor(() => expect(parent.value).toBe(uuidFor("ef-west")));
  });
});
