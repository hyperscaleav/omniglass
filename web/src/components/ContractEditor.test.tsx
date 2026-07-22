import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import ContractEditor from "./ContractEditor";
import { classifierPropertiesKey, type ClassifierKind, type ClassifierProperty } from "../lib/classifier_properties";
import { PROPERTIES_KEY, type PropertyRow } from "../lib/properties";
import { ME_KEY, type Me } from "../lib/auth";

// One editor curates the declared-properties contract of all three classifiers: a
// product (for its components), a standard (for the systems conforming to it), and
// a location type (for the locations of that type). These cover the two arcs the
// product editor's own test does not, plus the seam itself: the kind picks the API
// route, the authorization resource, and the copy. Data is seeded into the query
// cache so no server is needed; the PUT / DELETE fetches are faked where a test
// drives them.
const contract: ClassifierProperty[] = [
  { property_name: "seat_count", required: true },
  { property_name: "display_count", default_value: 2, required: false },
];

const catalog: PropertyRow[] = [
  { name: "seat_count", data_type: "int", display_name: "Seat count", official: true },
  { name: "display_count", data_type: "int", display_name: "Display count", official: true },
  { name: "has_camera", data_type: "bool", display_name: "Has camera", official: true },
];

const owner: Me = { principal: { id: "p", kind: "human" }, permissions: [">"], grants: [] };
// A reader of everything: it can see a contract but never write one.
const reader: Me = { principal: { id: "r", kind: "human" }, permissions: ["*:read"], grants: [] };

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function mount(kind: ClassifierKind, opts: { me?: Me; official?: boolean; lines?: ClassifierProperty[] } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...classifierPropertiesKey(kind, "meeting-room")], opts.lines ?? contract);
  qc.setQueryData([...PROPERTIES_KEY], catalog);
  qc.setQueryData([...ME_KEY], opts.me ?? owner);
  return render(() => (
    <QueryClientProvider client={qc}>
      <ContractEditor classifier={kind} id="meeting-room" official={opts.official ?? false} />
    </QueryClientProvider>
  ));
}

describe("ContractEditor on a standard", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists each declared property with its default and required marker", () => {
    const { getByText } = mount("standard");
    expect(getByText("Declared properties")).toBeTruthy();
    expect(getByText("the standard contract")).toBeTruthy();
    // The copy names what conforms to a standard: a system, not a component.
    expect(getByText(/A system conforming to this standard inherits/)).toBeTruthy();
    expect(getByText("seat_count")).toBeTruthy();
    expect(getByText("2")).toBeTruthy(); // the declared default
    expect(getByText("no default")).toBeTruthy();
    expect(getByText("required")).toBeTruthy();
    expect(getByText("optional")).toBeTruthy();
  });

  it("declares a picked catalog property, PUTting to the standards contract route", async () => {
    let put: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PUT") {
        put = req.clone();
        return json({ property_name: "has_camera", default_value: true, required: false });
      }
      return json({ properties: contract });
    });

    const { getByLabelText } = mount("standard");
    // Only the properties NOT already declared are offered.
    const picker = getByLabelText("Property to declare") as HTMLSelectElement;
    expect(Array.from(picker.options).map((o) => o.value)).toEqual(["", "has_camera"]);

    fireEvent.change(picker, { target: { value: "has_camera" } });
    fireEvent.input(getByLabelText("Default for the new property"), { target: { value: "true" } });
    fireEvent.click(getByLabelText("Declare property"));

    await waitFor(() => expect(put).toBeTruthy());
    expect(put!.url).toContain("/standards/meeting-room/properties/has_camera");
    expect(await put!.json()).toEqual({ required: false, default_value: true }); // coerced to bool
  });

  it("edits a line in place, PUTting the revised default", async () => {
    let put: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PUT") {
        put = req.clone();
        return json({ property_name: "display_count", default_value: 3, required: false });
      }
      return json({ properties: contract });
    });

    const { getByLabelText } = mount("standard");
    fireEvent.click(getByLabelText("Edit display_count"));
    const input = getByLabelText("Default for display_count") as HTMLInputElement;
    expect(input.value).toBe("2"); // seeded from the declared default
    fireEvent.input(input, { target: { value: "3" } });
    fireEvent.click(getByLabelText("Save display_count"));

    await waitFor(() => expect(put).toBeTruthy());
    expect(put!.url).toContain("/standards/meeting-room/properties/display_count");
    expect(await put!.json()).toEqual({ required: false, default_value: 3 });
  });

  it("withdraws a line after confirmation, calling DELETE", async () => {
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);
    let del: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "DELETE") {
        del = req.clone();
        return new Response(null, { status: 204 });
      }
      return json({ properties: contract });
    });

    const { getByLabelText } = mount("standard");
    fireEvent.click(getByLabelText("Withdraw seat_count"));

    await waitFor(() => expect(del).toBeTruthy());
    expect(confirmSpy.mock.calls[0][0]).toContain("standard's contract"); // the copy names the classifier
    expect(del!.url).toContain("/standards/meeting-room/properties/seat_count");
  });

  it("renders an official standard's contract read-only", () => {
    const { getByText, queryByLabelText } = mount("standard", { official: true });
    expect(getByText("seat_count")).toBeTruthy(); // the list still renders
    expect(getByText("seed-owned, read-only")).toBeTruthy();
    expect(queryByLabelText("Property to declare")).toBeNull();
    expect(queryByLabelText("Edit seat_count")).toBeNull();
    expect(queryByLabelText("Withdraw seat_count")).toBeNull();
  });

  it("hides the write controls from a principal without standard:update", () => {
    const { getByText, queryByLabelText } = mount("standard", { me: reader });
    expect(getByText("seat_count")).toBeTruthy();
    expect(queryByLabelText("Property to declare")).toBeNull();
    expect(queryByLabelText("Edit seat_count")).toBeNull();
    expect(queryByLabelText("Withdraw seat_count")).toBeNull();
  });

  it("shows the empty state when a standard declares nothing", () => {
    const { getByText } = mount("standard", { lines: [] });
    expect(getByText("This standard declares no properties.")).toBeTruthy();
  });
});

describe("ContractEditor on a location type", () => {
  afterEach(() => vi.restoreAllMocks());

  it("speaks location-type language over the location-types route", async () => {
    let put: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PUT") {
        put = req.clone();
        return json({ property_name: "has_camera", required: true });
      }
      return json({ properties: contract });
    });

    const { getByText, getByLabelText } = mount("location-type");
    expect(getByText("the location type contract")).toBeTruthy();
    expect(getByText(/A location of this type inherits/)).toBeTruthy();

    fireEvent.change(getByLabelText("Property to declare"), { target: { value: "has_camera" } });
    fireEvent.click(getByLabelText("Declare property"));

    await waitFor(() => expect(put).toBeTruthy());
    expect(put!.url).toContain("/location-types/meeting-room/properties/has_camera");
  });

  // A location type's contract is part of the type registry, so the server gates it
  // on type:update, not location:update. A principal holding only location writes
  // must not see the write controls.
  it("gates its write controls on type:update, not location:update", () => {
    const locationWriter: Me = { principal: { id: "l", kind: "human" }, permissions: ["*:read", "location:update", "location:delete"], grants: [] };
    const typeWriter: Me = { principal: { id: "t", kind: "human" }, permissions: ["*:read", "type:update", "type:delete"], grants: [] };
    expect(mount("location-type", { me: locationWriter }).queryByLabelText("Property to declare")).toBeNull();
    expect(mount("location-type", { me: typeWriter }).getByLabelText("Property to declare")).toBeTruthy();
  });

  it("shows the empty state when a location type declares nothing", () => {
    const { getByText } = mount("location-type", { lines: [] });
    expect(getByText("This location type declares no properties.")).toBeTruthy();
  });
});
