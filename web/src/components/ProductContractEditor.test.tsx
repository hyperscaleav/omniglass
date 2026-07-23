import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import ProductContractEditor from "./ProductContractEditor";
import { productPropertiesKey, type ProductProperty } from "../lib/product_properties";
import { PROPERTIES_KEY, type PropertyRow } from "../lib/properties";
import { ME_KEY, type Me } from "../lib/auth";

// The contract editor curates which catalog properties a product declares and what
// each defaults to. Data is seeded into the query cache so no server is needed; the
// PUT / DELETE fetches are faked where a test drives them.
const contract: ProductProperty[] = [
  { property_name: "serial_number", property_id: "serial_number-id", required: true },
  { property_name: "firmware_version", property_id: "firmware_version-id", default_value: "1.4.2", required: false },
];

const catalog: PropertyRow[] = [
  { name: "serial_number", data_type: "string", display_name: "Serial number", official: true },
  { name: "firmware_version", data_type: "string", display_name: "Firmware version", official: true },
  { name: "port_count", data_type: "int", display_name: "Port count", official: true },
];

const owner: Me = { principal: { id: "p", kind: "human" }, permissions: [">"], grants: [] };
const reader: Me = { principal: { id: "r", kind: "human" }, permissions: ["product:read"], grants: [] };

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function mount(opts: { me?: Me; official?: boolean; lines?: ProductProperty[] } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...productPropertiesKey("tsw-1070")], opts.lines ?? contract);
  qc.setQueryData([...PROPERTIES_KEY], catalog);
  qc.setQueryData([...ME_KEY], opts.me ?? owner);
  return render(() => (
    <QueryClientProvider client={qc}>
      <ProductContractEditor productId="tsw-1070" official={opts.official ?? false} />
    </QueryClientProvider>
  ));
}

describe("ProductContractEditor", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists each declared property with its default and required marker", () => {
    const { getByText, getAllByText } = mount();
    expect(getByText("Declared properties")).toBeTruthy();
    expect(getByText("serial_number")).toBeTruthy();
    expect(getByText("Serial number")).toBeTruthy();
    expect(getByText("firmware_version")).toBeTruthy();
    // The line with a default shows it; the line without reads "no default".
    expect(getByText("1.4.2")).toBeTruthy();
    expect(getByText("no default")).toBeTruthy();
    // serial_number is required, firmware_version is not.
    expect(getByText("required")).toBeTruthy();
    expect(getByText("optional")).toBeTruthy();
    expect(getAllByText("string").length).toBe(2); // the data_type badge per line
  });

  it("renders an official product's contract read-only (no declare, edit, or withdraw)", () => {
    const { getByText, queryByLabelText } = mount({ official: true });
    expect(getByText("serial_number")).toBeTruthy(); // the list still renders
    expect(getByText("seed-owned, read-only")).toBeTruthy();
    expect(queryByLabelText("Property to declare")).toBeNull();
    expect(queryByLabelText("Edit serial_number")).toBeNull();
    expect(queryByLabelText("Withdraw serial_number")).toBeNull();
  });

  it("hides the write controls from a principal without product:update", () => {
    const { getByText, queryByLabelText } = mount({ me: reader });
    expect(getByText("serial_number")).toBeTruthy();
    expect(queryByLabelText("Property to declare")).toBeNull();
    expect(queryByLabelText("Edit serial_number")).toBeNull();
    expect(queryByLabelText("Withdraw serial_number")).toBeNull();
  });

  it("declares a picked catalog property, PUTting the default and required flag", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const method = typeof input === "string" ? "GET" : req.method;
      if (method === "PUT") return json({ property_name: "port_count", property_id: "port_count-id", default_value: 8, required: true });
      return json({ properties: contract });
    });

    const { getByLabelText } = mount();
    // Only the properties NOT already declared are offered.
    const picker = getByLabelText("Property to declare") as HTMLSelectElement;
    expect(Array.from(picker.options).map((o) => o.value)).toEqual(["", "port_count"]);

    fireEvent.change(picker, { target: { value: "port_count" } });
    fireEvent.input(getByLabelText("Default for the new property"), { target: { value: "8" } });
    fireEvent.click(getByLabelText("Declare property"));

    await waitFor(() => {
      const put = vi.mocked(fetch).mock.calls.find(([i]) => (i as Request)?.method === "PUT");
      expect(put).toBeTruthy();
      expect((put![0] as Request).url).toContain("/products/tsw-1070/properties/port_count");
    });
  });

  it("edits a line in place, PUTting the revised default", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const method = typeof input === "string" ? "GET" : req.method;
      if (method === "PUT") return json({ property_name: "firmware_version", property_id: "firmware_version-id", default_value: "2.0.0", required: false });
      return json({ properties: contract });
    });

    const { getByLabelText } = mount();
    fireEvent.click(getByLabelText("Edit firmware_version"));
    const input = getByLabelText("Default for firmware_version") as HTMLInputElement;
    expect(input.value).toBe("1.4.2"); // seeded from the declared default
    fireEvent.input(input, { target: { value: "2.0.0" } });
    fireEvent.click(getByLabelText("Save firmware_version"));

    await waitFor(() => {
      const put = vi.mocked(fetch).mock.calls.find(([i]) => (i as Request)?.method === "PUT");
      expect(put).toBeTruthy();
      expect((put![0] as Request).url).toContain("/products/tsw-1070/properties/firmware_version");
    });
  });

  it("withdraws a line after confirmation, calling DELETE", async () => {
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const method = typeof input === "string" ? "GET" : req.method;
      if (method === "DELETE") return new Response(null, { status: 204 });
      return json({ properties: contract });
    });

    const { getByLabelText } = mount();
    fireEvent.click(getByLabelText("Withdraw serial_number"));

    await waitFor(() => {
      expect(confirmSpy).toHaveBeenCalled();
      const del = vi.mocked(fetch).mock.calls.find(([i]) => (i as Request)?.method === "DELETE");
      expect(del).toBeTruthy();
      expect((del![0] as Request).url).toContain("/products/tsw-1070/properties/serial_number");
    });
  });

  it("shows the empty state when a product declares nothing", () => {
    const { getByText } = mount({ lines: [] });
    expect(getByText("This product declares no properties.")).toBeTruthy();
  });
});
