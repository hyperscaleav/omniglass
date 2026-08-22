import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor, fireEvent, within } from "@solidjs/testing-library";
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
import { hueFor } from "../lib/system_color";
import { SYSTEM_VERDICTS_KEY } from "../lib/health";
import { NAME_MIN_W } from "../components/TreeList";

// The Systems page on the shared TreeList in the create-as-route model: New routes
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

  it("shows the system's type by label beside its standard", async () => {
    mount("/systems/boardroom?view=detail");
    await waitFor(() => expect(screen.getByText("Name")).toBeTruthy());
    expect(screen.getByText("Type")).toBeTruthy();
    // The registry's label, not the raw handle the row carries.
    expect(screen.getAllByText("Classroom").length).toBeGreaterThan(0);
    expect(screen.queryByText("class")).toBeNull();
  });

  it("sends system_type_id on save, always keyed so an unclassify actually clears", async () => {
    let sent: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PATCH" && req.url.includes("/systems/")) {
        sent = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({}), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({}), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount("/systems/boardroom?view=detail");
    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    const picker = (await waitFor(() => screen.getByLabelText("Type"))) as HTMLSelectElement;
    // Seeded from the row, then re-pointed at an ancestor node.
    expect(picker.value).toBe("class");
    fireEvent.change(picker, { target: { value: "room" } });
    fireEvent.click(screen.getByText("Save changes"));
    await waitFor(() => expect(sent).toBeTruthy());
    expect(sent).toMatchObject({ system_type_id: "room" });
  });

  it("wears a colour dot derived from its uuid on the list row", async () => {
    mount("/systems");
    await waitFor(() => expect(screen.getByText("Boardroom")).toBeTruthy());
    const dot = document.querySelector(".og-system-dot") as HTMLElement;
    expect(dot).toBeTruthy();
    expect(dot.style.getPropertyValue("--sys-h")).toBe(String(hueFor(sys.id)));
  });

  it("shows an existing system read-only in view: no tag add control, an Edit affordance", async () => {
    mount("/systems/boardroom?view=detail");
    // The detail resolves and renders the read-only facts.
    await waitFor(() => expect(screen.getByText("Name")).toBeTruthy());
    // No in-body mutation control in view: the TagAdder add row is absent.
    expect(screen.queryByPlaceholderText(/Add a tag/)).toBeNull();
    // The view footer offers Edit (which would flip the accordion to edit mode).
    expect(screen.getByText("Edit")).toBeTruthy();
  });

  it("edit mode exposes an editable name with a check button", async () => {
    mount("/systems/boardroom?view=detail");
    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    // The name becomes an editable input seeded from the row.
    const nameInput = (await screen.findByDisplayValue("boardroom")) as HTMLInputElement;
    expect(nameInput.disabled).toBe(false);
    // An inline check button sits beside it.
    expect(screen.getByLabelText("Check name")).toBeTruthy();
  });

  it("a fresh detail view keeps the name read-only", async () => {
    mount("/systems/boardroom?view=detail");
    await waitFor(() => expect(screen.getByText("Name")).toBeTruthy());
    // No check button until edit begins: the name is a read-only fact.
    expect(screen.queryByLabelText("Check name")).toBeNull();
  });

  it("shows the system's standard by label, not its id", async () => {
    mount("/systems/boardroom?view=detail");
    await waitFor(() => expect(screen.getByText("Name")).toBeTruthy());
    expect(screen.getAllByText("Meeting room").length).toBeGreaterThan(0);
  });

  // #627 Task 15c: routes take :id now. A name-shaped deep link resolves
  // through TreeList's byAddr fallback and corrects the address bar to the
  // uuid (replace); a rename afterward must leave that URL alone, since the
  // id it addresses never changes.
  it("redirects a name-shaped deep link to the resolved uuid, and a rename leaves the route where it is", async () => {
    mount("/systems/boardroom?view=detail");
    await waitFor(() => expect(window.location.pathname).toBe(`/systems/${sys.id}`));
    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    const nameInput = (await screen.findByDisplayValue("boardroom")) as HTMLInputElement;
    fireEvent.input(nameInput, { target: { value: "exec-boardroom" } });
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const { method, url } = req;
      if (method === "PATCH" && url.includes(`/systems/${sys.id}`)) {
        return new Response(JSON.stringify(sys), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (method === "POST" && url.includes(`/systems/${sys.id}:rename`)) {
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
    mount("/systems/no-such-system?view=detail");
    expect(await screen.findByText(/No such system/)).toBeTruthy();
    expect(screen.getByText(/old address/)).toBeTruthy();
    expect(screen.queryByText("Boardroom")).toBeNull();
  });

  // Review finding 1 (task-15-review.md #3): two systems sharing a name in
  // different placements is #627's own legal default outcome (#627 Task 10),
  // and the URL already carries the uuid, but the detail page's writes must
  // not fall back to n().raw.name (ErrAmbiguousName, mapped to a 409).
  it("addresses every write on the detail page by uuid, not by name, so a duplicate-named system stays editable", async () => {
    const twinA: System = { ...sys, id: uuidFor("s-twin-a"), name: "twin", location: "room-a", location_id: uuidFor("loc-room-a") };
    const twinB: System = { ...sys, id: uuidFor("s-twin-b"), name: "twin", location: "room-b", location_id: uuidFor("loc-room-b") };
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...SYSTEMS_KEY], [twinA, twinB]);
    qc.setQueryData([...LOCATIONS_KEY], []);
    qc.setQueryData([...COMPONENTS_KEY], []);
    qc.setQueryData([...STANDARDS_KEY], standards);
    qc.setQueryData([...SYSTEM_TYPES_KEY], systemTypes);
    qc.setQueryData([...ownerPropertiesKey("system", twinA.id)], []);
    qc.setQueryData([...ME_KEY], me);
    qc.setQueryData([...TAGS_KEY], []);
    qc.setQueryData([...entityTagsKey("system", twinA.id)], []);
    window.history.pushState({}, "", `/systems/${twinA.id}?view=detail`);
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="/systems" component={Systems} />
          <Route path="/systems/:id" component={Systems} />
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
      if (method === "PATCH" && url.includes(`/systems/${twinA.id}`)) {
        seen.push("patch");
        return new Response(JSON.stringify(twinA), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (method === "POST" && url.includes(`/systems/${twinA.id}:rename`)) {
        seen.push("rename");
        return new Response(JSON.stringify({ ...twinA, name: "twin-renamed" }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (method === "GET") {
        return new Response(JSON.stringify({ systems: [twinA, twinB] }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      throw new Error(`unexpected fetch in this test: ${method} ${url}`);
    });
    fireEvent.click(screen.getByText("Save changes"));
    // Wait for the save to fully resolve (edit mode exits), not just for the
    // fetch calls to have fired: the accordion's own save() still has an
    // awaited invalidateQueries after the rename resolves, so checking seen
    // alone races the view/edit mode switch the Delete button depends on.
    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    expect(seen).toContain("patch");
    expect(seen).toContain("rename");

    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const { method, url } = req;
      if (method === "DELETE" && url.includes(`/systems/${twinA.id}`)) {
        seen.push("delete");
        return new Response(null, { status: 204 });
      }
      if (method === "GET") {
        return new Response(JSON.stringify({ systems: [twinB] }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      throw new Error(`unexpected fetch in this test: ${method} ${url}`);
    });
    vi.spyOn(window, "confirm").mockReturnValue(true);
    fireEvent.click(screen.getByText("Delete"));
    await waitFor(() => expect(seen).toContain("delete"));
  });
});

// #627 scopes name uniqueness to placement, not the whole fleet: two systems
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
    qc.setQueryData([...SYSTEM_TYPES_KEY], systemTypes);
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

describe("Systems list health column (#627 review round 3, regression 3; #653)", () => {
  afterEach(() => {
    window.history.pushState({}, "", "/");
    vi.restoreAllMocks();
  });

  // Keying, unchanged in substance and moved to its new source. The column and
  // its sort read the page's ONE bulk verdict map (#653) instead of a per-row
  // query, and that map is keyed by UUID, matching where RolesPanel and
  // MembersPanel invalidate after a role or member write (#627 review finding 1:
  // the detail panels address by uuid, since a name is scoped to placement, not
  // the whole fleet). A name-keyed map would render nothing here, which is what
  // this asserts: the verdict is seeded ONLY at the uuid.
  it("reads the health column from the system's uuid, matching where RolesPanel and MembersPanel invalidate", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...SYSTEMS_KEY], [sys]);
    qc.setQueryData([...LOCATIONS_KEY], []);
    qc.setQueryData([...COMPONENTS_KEY], []);
    qc.setQueryData([...STANDARDS_KEY], standards);
    qc.setQueryData([...SYSTEM_TYPES_KEY], systemTypes);
    qc.setQueryData([...ME_KEY], me);
    qc.setQueryData([...TAGS_KEY], []);
    qc.setQueryData([...SYSTEM_VERDICTS_KEY], new Map([[sys.id, "healthy"]]));
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      throw new Error(`unexpected fetch in this test: ${req.method} ${req.url}`);
    });
    window.history.pushState({}, "", "/systems");
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="/systems" component={Systems} />
        </Router>
      </QueryClientProvider>
    ));
    await waitFor(() => expect(screen.getByText("healthy")).toBeTruthy());
  });

  // The measurement #653 is actually about, and the reason it is a REQUEST COUNT
  // rather than a screenshot: the rendered column looks identical either way, so
  // the only thing that can fail is the number of requests it took to paint it.
  //
  // Nothing is seeded. The page loads cold, exactly as an operator's first paint
  // does, and every request it makes is counted. Before this change the health
  // column fired one GET /systems/{id}/health per row, each resolving every role,
  // its occupants, their alarms and thirty days of transitions; a twelve-system
  // page cost twelve of them. It now costs ONE GET /systems:health for the page.
  it("paints a page of systems with one health request, not one per row", async () => {
    const many: System[] = Array.from({ length: 12 }, (_, i) => ({
      id: uuidFor(`bulk-${i}`),
      name: `room-${i}`,
      label: `Room ${i}`,
      member_count: 0,
      effective_tags: {},
    }));
    const verdicts = many.map((s, i) => ({ system: s.id, verdict: i % 3 === 0 ? "degraded" : "healthy" }));

    const urls: string[] = [];
    const body = (url: string): unknown => {
      if (url.includes("/systems:health")) return { verdicts };
      // The per-row read is served too, and deliberately: if it were not, the
      // old implementation would fail this test by rendering nothing rather than
      // by making twelve requests, and the number is the whole point. With both
      // answers available, the only thing that can distinguish the two is the
      // count.
      const perRowMatch = /\/systems\/([^/]+)\/health/.exec(url);
      if (perRowMatch) {
        const id = decodeURIComponent(perRowMatch[1]);
        return {
          owner: id, owner_kind: "system", roles: [], systems: [], transitions: [],
          verdict: verdicts.find((v) => v.system === id)?.verdict ?? "healthy",
        };
      }
      if (url.includes("/systems")) return { systems: many };
      if (url.includes("/locations")) return { locations: [] };
      if (url.includes("/components")) return { components: [] };
      if (url.includes("/standards")) return { standards };
      if (url.includes("/system-types")) return { system_types: systemTypes };
      if (url.includes("/tags")) return { tags: [] };
      if (url.includes("/auth/me")) return me;
      return {};
    };
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = (input as Request).url;
      urls.push(url);
      return new Response(JSON.stringify(body(url)), { status: 200, headers: { "Content-Type": "application/json" } });
    });

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    window.history.pushState({}, "", "/systems");
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="/systems" component={Systems} />
        </Router>
      </QueryClientProvider>
    ));

    // Wait for the column to have actually painted, so the count is taken after
    // the work is done rather than before it started: a request count read too
    // early is zero for every implementation, which is the shape of a test that
    // cannot fail.
    await waitFor(() => expect(screen.getAllByText("degraded").length).toBeGreaterThan(0));

    const perRow = urls.filter((u) => /\/systems\/[^/]+\/health/.test(u));
    expect(perRow).toEqual([]);
    const bulk = urls.filter((u) => u.includes("/systems:health"));
    expect(bulk).toHaveLength(1);
  });
});

// The Properties panel on the system detail is the shared owner panel, pointed at
// the system arc: the standard's contract resolved against the system's own values,
// with anything the system sets that no contract declares grouped off contract.
describe("Systems properties panel", () => {
  afterEach(() => window.history.pushState({}, "", "/"));

  it("resolves the standard's contract on the detail, off-contract values apart", async () => {
    mount("/systems/boardroom?view=detail");
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

    mount("/systems/boardroom?view=detail");
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
      // Addressed by uuid (#627 review finding 1), not the name the route
      // used to carry.
      expect(put!.url).toContain(`/systems/${sys.id}/properties/seat_count`);
      expect(JSON.parse(put!.body)).toEqual({ value: 8 }); // coerced to its data_type
    });
  });
});

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

// The Systems half of #690's uniformity clause. Systems declares the widest
// default set of the three (960px of columns), so it lost the Name column at a
// wider viewport than Components did: measured 0px at 1280, where the list card
// offers 973px. Same assertion as Components and Locations, because the whole
// point of the fix is that one shared rule now produces one behaviour.
describe("Systems list keeps a floor under the Name column (#690)", () => {
  it("declares no width on Name and a table floor that leaves it NAME_MIN_W", async () => {
    localStorage.clear();
    mount("/systems");
    await waitFor(() => expect(document.querySelector("table.og-rows")).toBeTruthy());

    const table = document.querySelector("table.og-rows") as HTMLTableElement;
    const cols = [...table.querySelectorAll("colgroup col")] as HTMLTableColElement[];
    const declared = cols.slice(1).reduce((sum, c) => sum + parseInt(c.style.width || "0", 10), 0);

    expect(cols[0].style.width).toBe("");
    expect(parseInt(table.style.minWidth, 10) - declared).toBe(NAME_MIN_W);
  });
});

// The label pen on the edit blade (#693). The "Generated" chip left the lists,
// and the fact landed beside the field an operator can act on, as the same lock
// the create form carries. This block proves the page is WIRED to the shared
// field (components/LabelPenField.test.tsx proves the field's own contract) and
// proves the half no component test can see: what a Save posts.
describe("Systems edit blade carries the label pen (#693)", () => {
  afterEach(() => window.history.pushState({}, "", "/"));

  // The fleet's own shape after #693: a rule renders "Boardroom 2" and the
  // platform holds the pen on it.
  const gen: System = { ...sys, id: uuidFor("s-gen"), name: "boardroom-2", label: "Boardroom 2", label_generated: true, member_count: 0 };

  function patchBodies(): Record<string, unknown>[] {
    const bodies: Record<string, unknown>[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PATCH") {
        bodies.push(JSON.parse(await req.clone().text()));
        return new Response(JSON.stringify(gen), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ systems: [gen], properties: [], standards, locations: [], components: [] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });
    return bodies;
  }

  it("opens the label locked on a row the platform labelled", async () => {
    mount(`/systems/${gen.id}?view=detail`, [gen]);
    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    const label = (await screen.findByLabelText("Label")) as HTMLInputElement;
    expect(label.value).toBe("Boardroom 2");
    expect(label.readOnly).toBe(true);
    expect(label.disabled).toBe(false);
    expect(screen.getByText(/Rendered from a label rule/)).toBeTruthy();
  });

  // The defect the lock exists to fix. The blade seeded a plain signal from
  // raw.label and posted `display() || undefined`, so opening the pencil
  // on a platform-labelled system and changing its STANDARD posted the
  // platform's own rendering back as an override, taking the pen silently: the
  // row stopped following its rule and nothing on screen said so.
  it("does not claim the pen when another field is saved", async () => {
    const bodies = patchBodies();
    mount(`/systems/${gen.id}?view=detail`, [gen]);
    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    await screen.findByLabelText("Label");
    fireEvent.change(screen.getByLabelText("Standard") as HTMLSelectElement, { target: { value: "huddle-space" } });
    fireEvent.click(screen.getByText("Save changes"));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0].standard_id).toBe("huddle-space");
    // "" is the API's "the platform's" (labelPen, #682), so the row keeps
    // following its rule across an unrelated edit.
    expect(bodies[0].label).toBe("");
  });

  it("posts the operator's words once they take the pen, seeded with what was on screen", async () => {
    const bodies = patchBodies();
    mount(`/systems/${gen.id}?view=detail`, [gen]);
    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    const label = (await screen.findByLabelText("Label")) as HTMLInputElement;
    fireEvent.click(screen.getByRole("button", { name: "Override the label" }));
    // Seeded rather than blanked: a blade has the label on screen already, so
    // taking the pen means amending it.
    expect(label.value).toBe("Boardroom 2");
    fireEvent.input(label, { target: { value: "Boardroom 2 (East)" } });
    fireEvent.click(screen.getByText("Save changes"));
    await waitFor(() => expect(bodies).toHaveLength(1));
    expect(bodies[0].label).toBe("Boardroom 2 (East)");
  });

  it("opens editable on a row the operator labelled, and hands it back on restore", async () => {
    const bodies = patchBodies();
    mount(`/systems/${sys.id}?view=detail`);
    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    const label = (await screen.findByLabelText("Label")) as HTMLInputElement;
    expect(label.value).toBe("Boardroom");
    expect(label.readOnly).toBe(false);
    fireEvent.click(screen.getByRole("button", { name: "Restore the label to default" }));
    expect(label.readOnly).toBe(true);
    expect(screen.getByText(/Handed back, so the platform renders this/)).toBeTruthy();
    fireEvent.click(screen.getByText("Save changes"));
    await waitFor(() => expect(bodies).toHaveLength(1));
    // The only way back from the console. Before this the field posted
    // `display() || undefined`, so clearing it left the label exactly as it was.
    expect(bodies[0].label).toBe("");
  });
});
