import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import type { JSX } from "solid-js";
import PropertiesPanel, { propertyResolutionBlade, ownerPropertyBladeId } from "./PropertiesPanel";
import { ownerPropertiesKey, type EffectiveProperty, type PropertyOwnerKind } from "../lib/owner_properties";
import { createEditSlot, type BladeEdit } from "../lib/blades";
import { ME_KEY, type Me } from "../lib/auth";

// One panel serves the three owner arcs. The component arc is pinned by
// PropertiesPanel.test.tsx (the regression check); these cover the seam itself for
// the system and location arcs: the owner kind picks the API route, the
// authorization resource, and the copy, and the resolution blade re-resolves the
// owner from the blade id alone. Rows are seeded into the query cache so no server
// is needed; the set / clear writes are faked where a test drives a Save.
const seed: EffectiveProperty[] = [
  // On contract, not overridden: the contract default is what applies.
  { property_name: "seat_count", display_name: "Seat count", data_type: "int", required: false, is_set: false, from_contract: true, default_value: 12, value: 12 },
  // On contract, overridden: the owner's value wins over the default.
  { property_name: "site.timezone", display_name: "Time zone", data_type: "string", required: false, is_set: true, from_contract: true, default_value: "UTC", set_value: "America/Denver", value: "America/Denver", value_id: "v-tz" },
  // Off contract: set directly on this owner, declared by no classifier.
  { property_name: "site.note", display_name: "Note", data_type: "string", required: false, is_set: true, from_contract: false, set_value: "leased", value: "leased", value_id: "v-note" },
];

const owner: Me = { principal: { id: "p", kind: "human" }, permissions: [">"], grants: [] };

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function client(kind: PropertyOwnerKind, name: string, rows: EffectiveProperty[] = seed) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ownerPropertiesKey(kind, name)], rows);
  qc.setQueryData([...ME_KEY], owner);
  return qc;
}

function mount(kind: PropertyOwnerKind, name: string, edit?: BladeEdit, rows: EffectiveProperty[] = seed) {
  const qc = client(kind, name, rows);
  const panel = () =>
    kind === "system" ? <PropertiesPanel system={name} edit={edit} /> : <PropertiesPanel location={name} edit={edit} />;
  return render(() => <QueryClientProvider client={qc}>{panel()}</QueryClientProvider>);
}

// The edit cell wraps the label row and the input below it.
const editCell = (label: HTMLElement) => (label.closest("div") as HTMLElement).parentElement as HTMLElement;

type Call = { method: string; url: string; body: string };
function captureWrites(): Call[] {
  const calls: Call[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const req = input as Request;
    let body = "";
    try { body = await req.clone().text(); } catch { body = ""; }
    calls.push({ method: req.method, url: req.url, body });
    if (req.method === "DELETE") return new Response(null, { status: 204 });
    if (req.method === "PUT") return json({ property_name: "x", value: null, value_id: "v" });
    return json({ properties: seed });
  });
  return calls;
}

describe("PropertiesPanel on a system", () => {
  afterEach(() => vi.restoreAllMocks());

  it("names the standard as the contract that declares the rows", () => {
    const { getByText, getByRole } = mount("system", "boardroom");
    expect(getByText("the standard contract, resolved")).toBeTruthy();
    const offContract = getByRole("group", { name: /off contract/i });
    expect(within(offContract).getByText("Note")).toBeTruthy();
    expect(getByText("set on this system, not declared by its standard")).toBeTruthy();
  });

  // A system whose standard simply declares no properties resolves empty too, so
  // the empty state must not claim the system conforms to no standard: that would
  // state something false on a system that plainly shows its standard above.
  it("explains where properties come from when nothing resolves, without claiming why", () => {
    const { getByText, queryByText } = mount("system", "boardroom", undefined, []);
    expect(getByText(/come from the standard it conforms to/i)).toBeTruthy();
    expect(queryByText(/conforms to no standard/i)).toBeNull();
  });

  it("writes an override and a clear to the system's own property routes on Save", async () => {
    const calls = captureWrites();
    const edit = createEditSlot();
    const { getByText } = mount("system", "boardroom", edit);
    edit.begin();

    const seats = editCell(getByText("Seat count"));
    fireEvent.click(within(seats).getByRole("checkbox")); // Override on
    fireEvent.input(within(seats).getByRole("spinbutton"), { target: { value: "8" } });

    const tz = editCell(getByText("Time zone"));
    fireEvent.click(within(tz).getByRole("checkbox")); // Override off: back to the default

    await edit.save();

    const puts = calls.filter((c) => c.method === "PUT");
    expect(puts.length).toBe(1); // untouched rows are not rewritten
    expect(puts[0].url).toContain("/systems/boardroom/properties/seat_count");
    expect(JSON.parse(puts[0].body)).toEqual({ value: 8 }); // coerced to its data_type

    const deletes = calls.filter((c) => c.method === "DELETE");
    expect(deletes.length).toBe(1);
    expect(deletes[0].url).toContain("/systems/boardroom/properties/site.timezone");
  });
});

describe("PropertiesPanel on a location", () => {
  afterEach(() => vi.restoreAllMocks());

  it("names the location type as the contract that declares the rows", () => {
    const { getByText } = mount("location", "hq");
    expect(getByText("the location type contract, resolved")).toBeTruthy();
    expect(getByText("set on this location, not declared by its location type")).toBeTruthy();
  });

  it("writes an override to the location's own property route on Save", async () => {
    const calls = captureWrites();
    const edit = createEditSlot();
    const { getByText } = mount("location", "hq", edit);
    edit.begin();

    const seats = editCell(getByText("Seat count"));
    fireEvent.click(within(seats).getByRole("checkbox"));
    fireEvent.input(within(seats).getByRole("spinbutton"), { target: { value: "20" } });

    await edit.save();

    const puts = calls.filter((c) => c.method === "PUT");
    expect(puts.length).toBe(1);
    expect(puts[0].url).toContain("/locations/hq/properties/seat_count");
  });

  // The panel is one component, so an owner that cannot be updated gets no input:
  // the write permission is the owner's own resource, resolved per kind.
  it("keeps the rows read-only for a caller without the owner's update permission", () => {
    const reader: Me = { principal: { id: "r", kind: "human" }, permissions: ["*:read"], grants: [] };
    const qc = client("location", "hq");
    qc.setQueryData([...ME_KEY], reader);
    const edit = createEditSlot();
    const { queryAllByRole } = render(() => (
      <QueryClientProvider client={qc}>
        <PropertiesPanel location="hq" edit={edit} />
      </QueryClientProvider>
    ));
    edit.begin();
    expect(queryAllByRole("checkbox").length).toBe(0);
  });
});

// The drill-in re-resolves the property from the blade id (owner kind, owner name,
// property name), so a system's blade reads the system's cache and speaks system
// language, never the component's.
const Body = propertyResolutionBlade.Body;

function withCache(kind: PropertyOwnerKind, name: string, ui: () => JSX.Element) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ownerPropertiesKey(kind, name)], seed);
  return render(() => <QueryClientProvider client={qc}>{ui()}</QueryClientProvider>);
}

const chainRow = (badge: HTMLElement) => badge.closest("div") as HTMLElement;

describe("propertyResolutionBlade across owners", () => {
  it("shadows the standard's default once the system overrides it", () => {
    const id = ownerPropertyBladeId({ kind: "system", name: "boardroom" }, "site.timezone");
    const { getByText } = withCache("system", "boardroom", () => <Body id={id} />);
    const def = getByText("contract default");
    expect(chainRow(def).querySelector(".line-through")).toBeTruthy();
    expect(chainRow(def).textContent).toContain("UTC");

    const self = getByText("this system");
    expect(self.className).toContain("badge-primary");
    expect(chainRow(self).textContent).toContain("America/Denver");
  });

  it("an off-contract property on a location says the location type declares nothing", () => {
    const id = ownerPropertyBladeId({ kind: "location", name: "hq" }, "site.note");
    const { getByText } = withCache("location", "hq", () => <Body id={id} />);
    expect(getByText("off contract")).toBeTruthy();
    expect(getByText("the location type does not declare this property")).toBeTruthy();
    expect(getByText(/removes the value from the location/)).toBeTruthy();
    expect(getByText("this location")).toBeTruthy();
  });
});
