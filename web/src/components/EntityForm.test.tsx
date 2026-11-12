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
import { systemRolesKey } from "../lib/system_roles";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";
import { COMPONENTS_KEY } from "../lib/components";
import { componentSystemsKey } from "../lib/members";

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

function mount(kind: "system" | "location", id: string, me: Me = owner, systemActions: string[] = ["create", "update", "delete"]) {
  return mountHost(kind, id, "page", me, systemActions);
}

function mountHost(kind: "system" | "location", id: string, host: "page" | "blade", me: Me = owner, systemActions: string[] = ["create", "update", "delete"]) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...SYSTEMS_KEY], [
    { id: uuidFor("ef-sys"), name: "huddle", label: "Huddle Room", standard: "huddle-room", system_type: "huddle", location: uuidFor("ef-room"), actions: systemActions },
  ]);
  qc.setQueryData([...LOCATIONS_KEY], [
    { id: uuidFor("ef-hq"), name: "hq", label: "Headquarters", location_type: "campus", parent_id: null, actions: ["create", "update", "delete"] },
    { id: uuidFor("ef-west"), name: "west", label: "West Building", location_type: "building", parent_id: uuidFor("ef-hq"), actions: ["create", "update", "delete"] },
    { id: uuidFor("ef-room"), name: "huddle-room", label: "Huddle Room", location_type: "room", parent_id: uuidFor("ef-west"), actions: ["create", "update", "delete"] },
  ]);
  qc.setQueryData([...LOCATION_TYPES_KEY], [
    { id: uuidFor("t-campus"), name: "campus", label: "Campus", allowed_parent_types: ["root"] },
    { id: uuidFor("t-building"), name: "building", label: "Building", allowed_parent_types: ["root", "campus"] },
    { id: uuidFor("t-room"), name: "room", label: "Room", allowed_parent_types: ["building", "campus"] },
  ]);
  qc.setQueryData([...STANDARDS_KEY], [{ id: uuidFor("std"), name: "huddle-room", label: "Huddle Room Standard" }]);
  qc.setQueryData([...SYSTEM_TYPES_KEY], [{ id: uuidFor("st"), name: "huddle", label: "Huddle" }]);
  qc.setQueryData([...TAGS_KEY], []);
  qc.setQueryData([...systemRolesKey(uuidFor("ef-sys"))], []);
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
                <EntityForm kind={kind} id={id} slot={slot} host={host} />
              </BladesContext.Provider>
            );
          }}
        />
      </Router>
    </QueryClientProvider>
  ));
  return { slot: () => slot, qc };
}

afterEach(() => { cleanup(); calls.length = 0; });

describe("the one form, read", () => {
  it("renders the sections from the row in order: identity, classification, placement, tags", async () => {
    mount("system", uuidFor("ef-sys"));
    const form = await screen.findByTestId("entity-form");
    const eyebrows = within(form).getAllByText(/^(Identity|Classification|Placement|Tags)$/).map((e) => e.textContent);
    expect(eyebrows).toEqual(["Identity", "Classification", "Placement", "Tags"]);
    // The registry's labels, never the handles (#870): the type and the
    // standard read as an operator names them.
    expect(within(form).getByText("Huddle")).toBeTruthy();
    expect(within(form).getByText("Huddle Room Standard")).toBeTruthy();
    expect(within(form).queryByText("huddle-room")).toBeNull();
    // Placement reads the location's label, resolved by id, so the label
    // shows twice: once as the system's own label, once as where it sits.
    expect(within(form).getAllByText("Huddle Room").length).toBe(2);
    expect(within(form).getByText("Where it sits")).toBeTruthy();
  });
});

describe("the one form reads labels, not handles (#870)", () => {
  it("names a location's type by its registry label", async () => {
    mount("location", uuidFor("ef-room"));
    const form = await screen.findByTestId("entity-form");
    expect(within(form).getByText("Room")).toBeTruthy();
    expect(within(form).queryByText("room")).toBeNull();
  });
});

describe("the one form in a blade is the glance (#872)", () => {
  // Configuration (roles, properties and their cascade) lives on the
  // workspace's Configure tab; the blade keeps what an operator who landed
  // from a row needs. A system's or component's place is the blade's own
  // place card, so the form does not say it a second time.
  it("renders identity, classification and tags, and none of the configuration panels", async () => {
    mountHost("system", uuidFor("ef-sys"), "blade");
    const form = await screen.findByTestId("entity-form");
    const eyebrows = within(form).getAllByText(/^(Identity|Classification|Placement|Tags|Roles|Properties)$/).map((e) => e.textContent);
    expect(eyebrows).toEqual(["Identity", "Classification", "Tags"]);
  });

  it("keeps a location's parent in the blade, where it is the place's own placement to move", async () => {
    mountHost("location", uuidFor("ef-room"), "blade");
    const form = await screen.findByTestId("entity-form");
    expect(within(form).getByText("Parent")).toBeTruthy();
  });

  it("keeps the configuration panels on the page host", async () => {
    mountHost("system", uuidFor("ef-sys"), "page");
    const form = await screen.findByTestId("entity-form");
    expect(within(form).getByText("Roles")).toBeTruthy();
  });
});

describe("the one form, gated by what the server says of this row", () => {
  // The permission says the caller may update systems SOMEWHERE; the row's own
  // actions say whether that reaches this one. A scoped operator reading a
  // system outside their update scope was offered Edit, and every Save was a
  // 403. The blade gated on the row before the form took the job over.
  it("does not offer edit on a row the caller's update does not reach", async () => {
    const { slot } = mount("system", uuidFor("ef-sys"), updaterOnly, []);
    await screen.findByTestId("entity-form");
    expect(slot().editable()).toBe(false);
  });

  // The server annotates a row with create, update and delete only
  // (internal/api/rowactions.go treeActions). Rename and move are not in that
  // vocabulary, so their absence from a row says nothing: reading it as "not
  // allowed" took rename and move away from the owner on every row.
  it("still offers rename and move, which the row's actions never name, to a caller holding them", async () => {
    const { slot } = mount("location", uuidFor("ef-room"), owner);
    const form = await screen.findByTestId("entity-form");
    slot().begin();
    await waitFor(() => expect(within(form).getByRole("button", { name: /check/i })).toBeTruthy());
  });
});

describe("the one form, saving", () => {
  // Explore, the blade title and the workspace header all read the fleet view,
  // not the kind's list. A save that refreshed only the list left every one of
  // them showing the old label until the window was refocused.
  it("refreshes the fleet view as well as the list, so what is drawn behind the form follows it", async () => {
    const { slot, qc } = mount("location", uuidFor("ef-room"));
    await screen.findByTestId("entity-form");
    const invalidated: string[] = [];
    const real = qc.invalidateQueries.bind(qc);
    qc.invalidateQueries = ((filters: { queryKey?: readonly unknown[] }) => {
      invalidated.push(String(filters?.queryKey?.[0]));
      return real(filters as never);
    }) as typeof qc.invalidateQueries;
    slot().begin();
    await slot().save();
    expect(invalidated).toContain("locations");
    expect(invalidated).toContain("fleet-view");
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

describe("a component's memberships, editable from its own face (#767)", () => {
  // A component's system on create is its primary MEMBERSHIP. The routes to
  // change it already exist (add, remove, setPrimary); the edit face is where
  // the operator who made the binding looks for it.
  const comp = uuidFor("mf-mic");
  const alpha = uuidFor("mf-alpha");
  const beta = uuidFor("mf-beta");
  const gamma = uuidFor("mf-gamma");
  const member = (system_id: string, system: string, primary: boolean) => ({ component: "mic", component_id: comp, component_verdict: "healthy", primary, system, system_id, system_count: 1 });
  const readerOnly: Me = { principal: { id: "u-rd", kind: "human" }, human: { username: "rd" }, permissions: ["component:read", "component:update", "system:read", "location:read", "tag:read", "product:read"], grants: [] };

  type Call = { method: string; path: string };
  function mountComponent(me: Me = owner, memberships = [member(alpha, "alpha", true), member(gamma, "gamma", false)]) {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...ME_KEY], me);
    qc.setQueryData([...SYSTEMS_KEY], [
      { id: alpha, name: "alpha", label: "Alpha Room", actions: ["update"] },
      { id: beta, name: "beta", label: "Beta Room", actions: ["update"] },
      { id: gamma, name: "gamma", label: "Gamma Room", actions: ["update"] },
    ]);
    qc.setQueryData([...COMPONENTS_KEY], [
      { id: comp, name: "mic", label: "Mic", product: "generic-device", system: "alpha", system_id: alpha, system_count: memberships.length, actions: ["create", "update", "delete"] },
    ]);
    qc.setQueryData([...componentSystemsKey(comp)], memberships);
    qc.setQueryData([...LOCATIONS_KEY], []);
    qc.setQueryData([...PRODUCTS_KEY], [{ id: uuidFor("mf-prod"), name: "generic-device", label: "Generic Device" }]);
    qc.setQueryData([...TAGS_KEY], []);
    window.history.pushState({}, "", "/web/x");
    let slot!: BladeEdit;
    render(() => (
      <QueryClientProvider client={qc}>
        <Router base="/web">
          <Route path="/x" component={() => {
            slot = createEditSlot();
            return <BladesContext.Provider value={createBladeController()}><EntityForm kind="component" id={comp} slot={slot} host="blade" /></BladesContext.Provider>;
          }} />
        </Router>
      </QueryClientProvider>
    ));
    return { slot: () => slot, qc };
  }

  // Answers every write 200/204, or refuses the one the test names.
  function stubWrites(refuse?: { method: string; match: string; status: number; detail: string }) {
    const calls: Call[] = [];
    const stub = vi.spyOn(globalThis, "fetch").mockImplementation(async (input: RequestInfo | URL) => {
      const req = input as Request;
      const path = decodeURIComponent(new URL(req.url).pathname.replace("/api/v1", ""));
      calls.push({ method: req.method, path });
      if (refuse && req.method === refuse.method && path.includes(refuse.match)) {
        return new Response(JSON.stringify({ title: "Forbidden", status: refuse.status, detail: refuse.detail }), { status: refuse.status, headers: { "content-type": "application/problem+json" } });
      }
      if (req.method === "PUT" || req.method === "DELETE" || path.endsWith(":setPrimary")) return new Response(null, { status: 204 });
      return new Response(JSON.stringify({}), { status: 200, headers: { "content-type": "application/json" } });
    });
    const writes = () => calls.filter((c) => c.path.includes("/members/"));
    return { calls, writes, stub };
  }

  it("shows the Memberships field in edit, seeded with the primary system and the others as chips", async () => {
    const { slot } = mountComponent();
    const form = await screen.findByTestId("entity-form");
    slot().begin();
    const primary = await within(form).findByRole("combobox", { name: /memberships/i }) as HTMLSelectElement;
    await waitFor(() => expect(primary.value).toBe(alpha));
    // The other membership is a chip with its own remove control.
    expect(within(form).getByRole("button", { name: /remove gamma room/i })).toBeTruthy();
  });

  it("picking another primary issues PUT then :setPrimary, addressed by uuid", async () => {
    const { writes, stub } = stubWrites();
    try {
      const { slot } = mountComponent();
      const form = await screen.findByTestId("entity-form");
      slot().begin();
      const primary = await within(form).findByRole("combobox", { name: /memberships/i });
      fireEvent.change(primary, { target: { value: beta } });
      await slot().save();
      expect(writes()).toEqual([
        { method: "PUT", path: `/systems/${beta}/members/${comp}` },
        { method: "POST", path: `/systems/${beta}/members/${comp}:setPrimary` },
      ]);
      expect(slot().editing()).toBe(false);
    } finally { stub.mockRestore(); }
  });

  it("promoting a system it is already in skips the PUT and only moves the primary", async () => {
    const { writes, stub } = stubWrites();
    try {
      const { slot } = mountComponent();
      const form = await screen.findByTestId("entity-form");
      slot().begin();
      fireEvent.change(await within(form).findByRole("combobox", { name: /memberships/i }), { target: { value: gamma } });
      await slot().save();
      expect(writes()).toEqual([{ method: "POST", path: `/systems/${gamma}/members/${comp}:setPrimary` }]);
    } finally { stub.mockRestore(); }
  });

  it("clearing the primary issues DELETE on that membership", async () => {
    const { writes, stub } = stubWrites();
    try {
      const { slot } = mountComponent(owner, [member(alpha, "alpha", true)]);
      const form = await screen.findByTestId("entity-form");
      slot().begin();
      fireEvent.change(await within(form).findByRole("combobox", { name: /memberships/i }), { target: { value: "" } });
      await slot().save();
      expect(writes()).toEqual([{ method: "DELETE", path: `/systems/${alpha}/members/${comp}` }]);
    } finally { stub.mockRestore(); }
  });

  it("removing a chip issues DELETE on that membership only", async () => {
    const { writes, stub } = stubWrites();
    try {
      const { slot } = mountComponent();
      const form = await screen.findByTestId("entity-form");
      slot().begin();
      fireEvent.click(await within(form).findByRole("button", { name: /remove gamma room/i }));
      await slot().save();
      expect(writes()).toEqual([{ method: "DELETE", path: `/systems/${gamma}/members/${comp}` }]);
    } finally { stub.mockRestore(); }
  });

  it("writes nothing when the memberships were not touched", async () => {
    const { writes, stub } = stubWrites();
    try {
      const { slot } = mountComponent();
      await screen.findByTestId("entity-form");
      slot().begin();
      await slot().save();
      expect(writes()).toEqual([]);
    } finally { stub.mockRestore(); }
  });

  it("is read-only without system:update, and still shows the memberships", async () => {
    const { slot } = mountComponent(readerOnly);
    const form = await screen.findByTestId("entity-form");
    slot().begin();
    await waitFor(() => expect(within(form).getByText("Alpha Room")).toBeTruthy());
    expect(within(form).getByText("Gamma Room")).toBeTruthy();
    expect(within(form).queryByRole("combobox", { name: /memberships/i })).toBeNull();
    expect(within(form).queryByRole("button", { name: /remove gamma room/i })).toBeNull();
    expect(within(form).getByText(/needs.*system:update/i)).toBeTruthy();
  });

  it("renders the server's refusal inline and leaves the face in edit", async () => {
    const { stub } = stubWrites({ method: "PUT", match: "/members/", status: 403, detail: "you may read system beta but not update it" });
    try {
      const { slot } = mountComponent();
      const form = await screen.findByTestId("entity-form");
      slot().begin();
      fireEvent.change(await within(form).findByRole("combobox", { name: /memberships/i }), { target: { value: beta } });
      await expect(slot().save()).rejects.toBeTruthy();
      expect((await within(form).findByRole("alert")).textContent).toContain("you may read system beta but not update it");
      expect(slot().editing()).toBe(true);
    } finally { stub.mockRestore(); }
  });

  it("refreshes both ends of the relation, so a system's member list follows without a reload", async () => {
    const { stub } = stubWrites();
    try {
      const { slot, qc } = mountComponent();
      const form = await screen.findByTestId("entity-form");
      const invalidated: string[] = [];
      const real = qc.invalidateQueries.bind(qc);
      qc.invalidateQueries = ((filters: { queryKey?: readonly unknown[] }) => {
        invalidated.push(String(filters?.queryKey?.[0]));
        return real(filters as never);
      }) as typeof qc.invalidateQueries;
      slot().begin();
      fireEvent.change(await within(form).findByRole("combobox", { name: /memberships/i }), { target: { value: beta } });
      await slot().save();
      expect(invalidated).toContain("system-members");
      expect(invalidated).toContain("component-systems");
      expect(invalidated).toContain("components");
    } finally { stub.mockRestore(); }
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
