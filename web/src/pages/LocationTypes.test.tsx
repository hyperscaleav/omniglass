import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import LocationTypes from "./LocationTypes";
import { LOCATION_TYPES_KEY, type LocationType } from "../lib/location_types";
import { classifierPropertiesKey, type ClassifierProperty } from "../lib/classifier_properties";
import { classifierMetricsKey } from "../lib/classifier_metrics";
import { PROPERTIES_KEY, type PropertyRow } from "../lib/properties";
import { METRICS_KEY } from "../lib/metric_types";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// The Location Types page is the place classifier registry alone on the shared
// FlatList: one registry, one query, no secret_type anywhere on its path (#598
// split the old joint Types page). A shipped row is official and editable, since
// the edit FORKS it (#703, ADR-0095); a custom row is writable for a caller
// holding location_type:*, and its detail carries the location type's
// declared-property contract. Data is seeded into the query cache so no server
// is needed (except where a test says otherwise).
const seed: LocationType[] = [
  { id: uuidFor("lt-campus"), name: "campus", display_name: "Campus", official: true, forked: false, icon: "map-pin", allowed_parent_types: [] },
  { id: uuidFor("lt-wing"), name: "wing", display_name: "Wing", official: false, forked: false, icon: "map-pin", allowed_parent_types: ["campus", "root"] },
  // A handle its display name does not contain, so filtering by it can only
  // succeed through the name field (the addressing-honesty test below).
  { id: uuidFor("lt-server-room"), name: "server-room", display_name: "Machine hall", official: false, forked: false, icon: "map-pin", allowed_parent_types: [] },
];

// The location type contract shown on the wing blade, plus the catalog the editor
// joins each line to for its display name and data type.
const wingContract: ClassifierProperty[] = [{ property_type_name: "floor_area_sqm", property_type_id: "floor_area_sqm-id", default_value: 40, required: false }];
const catalog: PropertyRow[] = [
  { name: "floor_area_sqm", data_type: "int", display_name: "Floor area", official: true },
  { name: "seat_count", data_type: "int", display_name: "Seat count", official: true },
];

const asides = () => document.querySelectorAll("aside[data-blade]");

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...LOCATION_TYPES_KEY], seed);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...PROPERTIES_KEY], catalog);
  qc.setQueryData([...classifierPropertiesKey("location-type", "wing")], wingContract);
  qc.setQueryData([...classifierPropertiesKey("location-type", "campus")], []);
  // The metric lane mounts beside the property one; empty contracts and an
  // empty catalog keep it inert without a server.
  qc.setQueryData([...METRICS_KEY], []);
  qc.setQueryData([...classifierMetricsKey("location-type", "wing")], []);
  qc.setQueryData([...classifierMetricsKey("location-type", "campus")], []);
  return render(() => (
    <QueryClientProvider client={qc}>
      <LocationTypes />
    </QueryClientProvider>
  ));
}

describe("LocationTypes page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists the registry rows, alphabetical by display name", () => {
    mount();
    expect(screen.getByText("campus")).toBeTruthy();
    expect(screen.getByText("wing")).toBeTruthy();
    const cells = screen.getAllByRole("cell").map((c) => c.textContent ?? "");
    const campusAt = cells.findIndex((t) => t.includes("Campus"));
    const hallAt = cells.findIndex((t) => t.includes("Machine hall"));
    expect(campusAt).toBeGreaterThanOrEqual(0);
    expect(hallAt).toBeGreaterThan(campusAt);
  });

  // One identity column carries both operator-facing identities (the display name
  // above, the name beneath).
  it("carries the display name and the name in a single Name column", () => {
    mount();
    const headers = screen.getAllByRole("columnheader").map((h) => h.textContent?.trim());
    expect(headers).toContain("Name");
    expect(headers.filter((h) => h === "Name")).toHaveLength(1);
    const cell = screen.getByText("server-room").closest("td");
    expect(cell?.textContent).toContain("Machine hall");
  });

  it("shows New location type for a caller holding location_type:create", () => {
    mount(admin);
    expect(screen.getByText("New location type")).toBeTruthy();
  });

  it("hides New location type for a caller without location_type:create", () => {
    mount(viewer);
    expect(screen.queryByText("New location type")).toBeNull();
  });

  it("shows the allowed-parents editor on the create form, with a Root option", async () => {
    mount();
    fireEvent.click(screen.getByText("New location type"));
    expect(await screen.findByText("Allowed parents")).toBeTruthy();
    expect(screen.getByText("Root (no parent)")).toBeTruthy();
  });

  // The create form leads with the display name and lets the name follow it, so an
  // operator never has to think about the character class. A hand-edit claims the
  // name, and relabelling after that leaves it alone.
  it("derives the name from the display name until the operator edits it", async () => {
    mount();
    fireEvent.click(screen.getByText("New location type"));
    const display = (await screen.findByPlaceholderText("Wing")) as HTMLInputElement;
    const name = screen.getByPlaceholderText("wing") as HTMLInputElement;

    fireEvent.input(display, { target: { value: "Server Room" } });
    expect(name.value).toBe("server-room");
    expect(screen.getByText(/Derived from the display name/)).toBeTruthy();

    fireEvent.input(name, { target: { value: "svr" } });
    fireEvent.input(display, { target: { value: "Server Room B" } });
    expect(name.value).toBe("svr");
    // The field says so too: the name is the operator's now, not derived.
    expect(screen.queryByText(/Derived from the display name/)).toBeNull();
  });

  // Regression for the nested-<label> bug: the picker used to live inside the
  // shared Field component, whose root is a for-less <label>. A click anywhere
  // in that label (the heading, the hint) forwards to the first labelable
  // descendant, which was the Root checkbox. The heading and hint must render
  // as plain, non-label text outside any wrapping <label>.
  it("clicking the Allowed parents heading or hint does not check Root", async () => {
    mount();
    fireEvent.click(screen.getByText("New location type"));
    await screen.findByText("Allowed parents");
    const root = screen.getByLabelText("Root (no parent)") as HTMLInputElement;
    expect(root.checked).toBe(false);
    fireEvent.click(screen.getByText("Allowed parents"));
    expect(root.checked).toBe(false);
    fireEvent.click(screen.getByText(/Leave every box unchecked/));
    expect(root.checked).toBe(false);
  });

  it("edit blade pre-checks the existing allowed_parent_types and saving sends that set", async () => {
    let sent: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PATCH" && req.url.includes("/location-types/wing")) {
        sent = JSON.parse(await req.clone().text());
        return new Response(
          JSON.stringify({ id: uuidFor("lt-wing"), name: "wing", display_name: "Wing", official: false, forked: false, icon: "map-pin", allowed_parent_types: ["campus", "root"] }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }
      // The post-save invalidation refetches the registry query alone.
      return new Response(JSON.stringify({ location_types: [] }), { status: 200, headers: { "Content-Type": "application/json" } });
    });

    mount();
    fireEvent.click(screen.getByText("wing"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));

    // Round-trip: the existing set (campus, root) arrives pre-checked.
    const root = within(blade).getByLabelText("Root (no parent)") as HTMLInputElement;
    const campus = within(blade).getByLabelText(/Campus/) as HTMLInputElement;
    expect(root.checked).toBe(true);
    expect(campus.checked).toBe(true);

    fireEvent.click(within(blade).getByText("Save"));
    await waitFor(() => expect(sent).toBeTruthy());
    expect(sent).toHaveProperty("allowed_parent_types");
    expect(sent).toMatchObject({ allowed_parent_types: expect.arrayContaining(["campus", "root"]) });
    expect((sent as { allowed_parent_types: string[] }).allowed_parent_types).toHaveLength(2);
  });

  it("read-only blade shows allowed parents by display name, with the root sentinel labeled", async () => {
    mount(viewer);
    fireEvent.click(screen.getByText("wing"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    // Read-only (the Edit pair is greyed by the lock): wing's set is [campus,
    // root]. The chip resolves the type name to its display name (Campus, not
    // the raw "campus"), and the sentinel renders as "Root", so the two read
    // consistently.
    expect((within(blade).getByLabelText("Edit") as HTMLButtonElement).disabled).toBe(true);
    expect(within(blade).getByText("Campus")).toBeTruthy();
    expect(within(blade).getByText("Root")).toBeTruthy();
    expect(within(blade).queryByText("campus")).toBeNull();
    // No pencil means no route into edit mode: the contract panels stay facts.
    expect(within(blade).queryByLabelText("Property to declare")).toBeNull();
    expect(within(blade).queryByLabelText("Withdraw floor_area_sqm")).toBeNull();
  });

  // The permission arm of the lock: a custom row a viewer cannot write greys
  // the Edit / Delete pair in place, each side naming the permission that
  // would unlock THAT button, instead of hiding the affordances.
  it("greys Edit and Delete for a viewer on a custom row, each naming its own missing permission", async () => {
    mount(viewer);
    fireEvent.click(screen.getByText("wing"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    const editBtn = within(blade).getByLabelText("Edit") as HTMLButtonElement;
    const deleteBtn = within(blade).getByText("Delete").closest("button") as HTMLButtonElement;
    expect(editBtn.disabled).toBe(true);
    expect(deleteBtn.disabled).toBe(true);
    expect(editBtn.closest(".tooltip")?.getAttribute("data-tip")).toBe("Requires location_type:update");
    expect(deleteBtn.closest(".tooltip")?.getAttribute("data-tip")).toBe("Requires location_type:delete");
  });

  // The location type is a classifier: its blade carries the declared-property
  // contract every location of the type resolves against, on the shared
  // ContractEditor (the same panel a product and a standard use).
  it("shows the location type's declared-property contract on its blade", async () => {
    mount();
    fireEvent.click(screen.getByText("wing"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect(within(blade).getByText("Declared properties")).toBeTruthy();
    // Both catalog lanes mount: the property contract and its metric sibling.
    expect(within(blade).getByText("Declared metrics")).toBeTruthy();
    expect(within(blade).getAllByText("the location type contract")).toHaveLength(2);
    expect(within(blade).getByText(/A location of this type inherits every property/)).toBeTruthy();
    expect(within(blade).getByText("floor_area_sqm")).toBeTruthy();
    expect(within(blade).getByText("40")).toBeTruthy(); // the declared default
    // Writable for this caller (owner) once the pencil flips edit mode (#621),
    // so the picker offers what is not declared.
    fireEvent.click(within(blade).getByLabelText("Edit"));
    const picker = within(blade).getByLabelText("Property to declare") as HTMLSelectElement;
    expect(Array.from(picker.options).map((o) => o.value)).toEqual(["", "seat_count"]);
  });

  // This assertion INVERTED with #703, deliberately, and it now guards the
  // capability the ownership flip would otherwise have withdrawn in silence.
  // It used to read "renders an official location type's contract read-only",
  // asserting the "official, read-only" hint on both lanes. A location type's
  // contract is not release-owned: nothing seeds a line, every one is the
  // operator's, and the gateway writes them on a shipped type on purpose. What
  // keeps the panel quiet on a shipped row is the blade's edit mode, the same
  // rule that governs a custom row, not the official flag.
  it("keeps a shipped location type's contract writable, gated by edit mode rather than by official", async () => {
    mount();
    fireEvent.click(screen.getByText("campus"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect(within(blade).queryAllByText("official, read-only")).toHaveLength(0);
    expect(within(blade).getByText("This location type declares no properties.")).toBeTruthy();
    // Read mode still offers nothing; the pencil is the one route to the controls.
    expect(within(blade).queryByLabelText("Property to declare")).toBeNull();
    fireEvent.click(within(blade).getByLabelText("Edit"));
    expect(within(blade).getByLabelText("Property to declare")).toBeTruthy();
  });

  // The three-state origin a fork-adopting registry shows, and the one action
  // that only a forked shipped row carries.
  it("reads origin three ways and offers Restore only on a forked shipped row", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...LOCATION_TYPES_KEY], [
      ...seed,
      { id: uuidFor("lt-room"), name: "room", display_name: "Room", official: true, forked: true, icon: "map-pin", allowed_parent_types: [] },
    ]);
    qc.setQueryData([...ME_KEY], admin);
    qc.setQueryData([...PROPERTIES_KEY], catalog);
    qc.setQueryData([...METRICS_KEY], []);
    qc.setQueryData([...classifierPropertiesKey("location-type", "room")], []);
    qc.setQueryData([...classifierMetricsKey("location-type", "room")], []);
    render(() => (
      <QueryClientProvider client={qc}>
        <LocationTypes />
      </QueryClientProvider>
    ));
    const cells = screen.getAllByRole("cell").map((c) => c.textContent ?? "");
    expect(cells.some((t) => t.includes("official"))).toBe(true);
    expect(cells.some((t) => t.includes("custom"))).toBe(true);
    expect(cells.some((t) => t.includes("overridden"))).toBe(true);

    // A pristine shipped row: editable (the edit forks it), nothing to discard.
    fireEvent.click(screen.getByText("campus"));
    let blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));
    expect(within(blade).queryByText("Restore shipped")).toBeNull();
    // Delete is present but greyed with the official sentence: a shipped row is
    // never deleted, and while it is pristine there is nothing to discard either.
    const del = within(blade).getByText("Delete").closest("button") as HTMLButtonElement;
    expect(del.disabled).toBe(true);

    // The forked one offers Restore in the same slot a custom row puts Delete.
    // Scoped to the table: the open blade's allowed-parents picker lists every
    // type by name too, so a bare query matches twice.
    fireEvent.click(within(screen.getByRole("table")).getByText("room"));
    blade = await waitFor(() => {
      // The newest blade on the stack: the campus one above is still open.
      const els = asides();
      const el = els[els.length - 1];
      if (!el || el.getAttribute("aria-labelledby")?.includes("campus")) throw new Error("no room blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));
    expect(within(blade).getByText("Restore shipped")).toBeTruthy();
  });

  // #692 on the console: the exit from platform naming, spelled the one way the
  // wire spells it (the field named in update_mask, no rule in the body).
  it("turns naming off with a masked clear", async () => {
    let sent: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PATCH" && req.url.includes("/location-types/deck")) {
        sent = JSON.parse(await req.clone().text());
        return new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ location_types: [] }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    vi.spyOn(globalThis, "confirm").mockReturnValue(true);

    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...LOCATION_TYPES_KEY], [
      { id: uuidFor("lt-deck"), name: "deck", display_name: "Deck", official: false, forked: false, icon: "map-pin", allowed_parent_types: [], name_rule: { stem: "" } },
    ]);
    qc.setQueryData([...ME_KEY], admin);
    qc.setQueryData([...PROPERTIES_KEY], catalog);
    qc.setQueryData([...METRICS_KEY], []);
    qc.setQueryData([...classifierPropertiesKey("location-type", "deck")], []);
    qc.setQueryData([...classifierMetricsKey("location-type", "deck")], []);
    render(() => (
      <QueryClientProvider client={qc}>
        <LocationTypes />
      </QueryClientProvider>
    ));
    fireEvent.click(screen.getByText("deck"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect(within(blade).getByText(/Positional: named 1, 2, 3/)).toBeTruthy();
    fireEvent.click(within(blade).getByLabelText("Edit"));
    fireEvent.click(within(blade).getByText("Turn naming off"));
    await waitFor(() => expect(sent).toBeTruthy());
    expect(sent).toEqual({ update_mask: ["name_rule"] });
  });

  // The blade model (#621): a blade opens read-only, and EVERY mutating control,
  // the contract panels' declare picker and per-line edit/withdraw included,
  // appears only after the pencil flips the blade into edit mode. Read mode
  // renders the declared lines as facts alone.
  it("keeps the contract's declare/edit/withdraw controls out of read mode until the pencil flips edit", async () => {
    mount();
    fireEvent.click(screen.getByText("wing"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    // Read mode: the declared line renders as a fact row...
    expect(within(blade).getByText("floor_area_sqm")).toBeTruthy();
    expect(within(blade).getByText("40")).toBeTruthy();
    // ...and nothing shaped like a mutating control renders, even for an admin.
    expect(within(blade).queryByLabelText("Property to declare")).toBeNull();
    expect(within(blade).queryByLabelText("Metric to declare")).toBeNull();
    expect(within(blade).queryByLabelText("Edit floor_area_sqm")).toBeNull();
    expect(within(blade).queryByLabelText("Withdraw floor_area_sqm")).toBeNull();
    // The pencil enters edit mode; the existing controls return as they were.
    fireEvent.click(within(blade).getByLabelText("Edit"));
    expect(within(blade).getByLabelText("Property to declare")).toBeTruthy();
    expect(within(blade).getByLabelText("Edit floor_area_sqm")).toBeTruthy();
    expect(within(blade).getByLabelText("Withdraw floor_area_sqm")).toBeTruthy();
  });

  it("declares a property on a location type, PUTting to the location-types contract route", async () => {
    let put: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PUT") {
        put = req.clone();
        return new Response(JSON.stringify({ property_type_name: "seat_count", property_type_id: "seat_count-id", default_value: 12, required: false }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }
      return new Response(JSON.stringify({ properties: wingContract }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });

    mount();
    fireEvent.click(screen.getByText("wing"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    // The declare row lives behind the blade's pencil (#621).
    fireEvent.click(within(blade).getByLabelText("Edit"));
    fireEvent.change(within(blade).getByLabelText("Property to declare"), { target: { value: "seat_count" } });
    fireEvent.input(within(blade).getByLabelText("Default for the new property"), { target: { value: "12" } });
    fireEvent.click(within(blade).getByLabelText("Declare property"));

    await waitFor(() => expect(put).toBeTruthy());
    expect(put!.url).toContain("/location-types/wing/properties/seat_count");
    expect(await put!.json()).toEqual({ required: false, default_value: 12 }); // coerced to the int data_type
  });
});

// #598: the old joint Types page fanned /location-types and /secret-types out
// behind one query and threw if EITHER failed, so a plain *:read viewer (no
// secret:read; secret is a sensitive resource off the viewer floor) lost the
// location registry it was entitled to. The split page must render from
// /location-types alone and never have /secret-types on its path.
describe("LocationTypes for a viewer-floor principal (#598)", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders the registry from /location-types alone, never fetching /secret-types", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.url.includes("/location-types")) {
        return new Response(
          JSON.stringify({ location_types: [{ id: uuidFor("lt-campus"), name: "campus", display_name: "Campus", official: false, forked: false, icon: "map-pin", allowed_parent_types: [] }] }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      }
      // Anything else is what the server would say to this viewer: forbidden.
      return new Response(JSON.stringify({ title: "Forbidden", status: 403 }), { status: 403, headers: { "Content-Type": "application/json" } });
    });
    // No seeded registry cache: the page fetches, as the console does live.
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...ME_KEY], viewer);
    render(() => (
      <QueryClientProvider client={qc}>
        <LocationTypes />
      </QueryClientProvider>
    ));
    expect(await screen.findByText("campus")).toBeTruthy();
    const urls = fetchMock.mock.calls.map((c) => (c[0] as Request).url);
    expect(urls.some((u) => u.includes("/secret-types"))).toBe(false);
  });
});

// The catalog addresses rows by the name (ADR-0062): the first column
// shows it, and the substring filter matches it. The server-room fixture's
// display name ("Machine hall") does not contain the handle, so the filter can
// only find it through the name field.
describe("LocationTypes addressing honesty (#469)", () => {
  afterEach(() => vi.restoreAllMocks());

  it("shows the handle in the Name column and finds the row by it in the filter", async () => {
    mount();
    expect(await screen.findByText("Machine hall")).toBeTruthy();
    const row = screen.getByText("Machine hall").closest("tr")!;
    expect(within(row).getByText("server-room")).toBeTruthy();
    const input = screen.getByRole("combobox") as HTMLInputElement;
    fireEvent.input(input, { target: { value: "server-room" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(screen.getByText("Machine hall")).toBeTruthy();
    expect(screen.queryByText("Campus")).toBeNull();
  });
});

// #581. The list renders the display name as the primary line (identityColumn),
// so the blade heading must be the words the row showed, not the identifier.
describe("LocationTypes blade heading", () => {
  afterEach(() => vi.restoreAllMocks());

  it("heads the blade with the words the row showed, not the identifier", async () => {
    mount();
    fireEvent.click(await screen.findByText("Machine hall"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    const heading = blade.querySelector("header .truncate") as HTMLElement;
    expect(heading.textContent).toBe("Machine hall");
  });
});
