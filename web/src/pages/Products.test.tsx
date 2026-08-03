import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Products from "./Products";
import { PRODUCTS_KEY, type Product } from "../lib/products";
import { VENDORS_KEY, type Vendor } from "../lib/vendors";
import { DRIVERS_KEY, type Driver } from "../lib/drivers";
import { CAPABILITIES_KEY, type Capability } from "../lib/capabilities";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// Products is the product catalog on the flat FlatList surface (the model a
// component is an instance of). An official (seed-owned) row is read-only, same
// invariant as the Types catalog's official rows: no edit pencil, no Delete.
// Data is seeded into the query cache so no server is needed; the vendor,
// driver, and capability registries the pickers read are seeded too, so the
// create form stays network-free.
// Wire truth (api/products.go): vendor/driver/parent arrive as BOTH the kebab
// handle (vendor) and the uuid (vendor_id); capabilities is a list of NAMES.
const seed: Product[] = [
  { id: uuidFor("prod-crestron-tsw-1070"), name: "crestron-tsw-1070", display_name: "Crestron TSW-1070", kind: "device", vendor: "crestron", vendor_id: uuidFor("ven-crestron"), driver: "crestron-ct", driver_id: uuidFor("drv-crestron-ct"), capabilities: ["touchscreen"], official: true },
  { id: uuidFor("prod-acme-panel"), name: "acme-panel", display_name: "Acme Panel", kind: "device", vendor: "acme-av", vendor_id: uuidFor("ven-acme-av"), capabilities: ["touchscreen"], official: false },
];

const vendors: Vendor[] = [
  { id: uuidFor("ven-crestron"), name: "crestron", display_name: "Crestron", kind: "manufacturer", official: true },
  { id: uuidFor("ven-acme-av"), name: "acme-av", display_name: "Acme AV", kind: "integrator", official: false },
];
const drivers: Driver[] = [{ id: uuidFor("drv-crestron-ct"), name: "crestron-ct", display_name: "Crestron CT", official: true }];
const capabilities: Capability[] = [{ id: uuidFor("cap-touchscreen"), name: "touchscreen", display_name: "Touchscreen", official: true }];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

const asides = () => document.querySelectorAll("aside[data-blade]");

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...PRODUCTS_KEY], seed);
  qc.setQueryData([...VENDORS_KEY], vendors);
  qc.setQueryData([...DRIVERS_KEY], drivers);
  qc.setQueryData([...CAPABILITIES_KEY], capabilities);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Products />
    </QueryClientProvider>
  ));
}

describe("Products page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists a seeded product, an official row has no edit/delete, and create opens for an admin", async () => {
    mount();
    expect(await screen.findByText("Crestron TSW-1070")).toBeInTheDocument();

    // official row has no edit/delete
    fireEvent.click(screen.getByText("Crestron TSW-1070"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect(within(blade).queryByRole("button", { name: /delete/i })).not.toBeInTheDocument();
    expect(within(blade).queryByLabelText("Edit")).not.toBeInTheDocument();

    // create is available to an admin. The label match is anchored because the
    // Name field's hint mentions the display name too.
    fireEvent.click(screen.getByRole("button", { name: /new product/i }));
    expect(screen.getByLabelText(/^Display name$/)).toBeInTheDocument();
  });

  it("a custom (non-official) row carries edit and delete", async () => {
    mount();
    fireEvent.click(screen.getByText("Acme Panel"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect(within(blade).getByLabelText("Edit")).toBeInTheDocument();
    fireEvent.click(within(blade).getByLabelText("Edit"));
    expect(within(blade).getByRole("button", { name: /delete/i })).toBeInTheDocument();
  });

  it("editing a custom row exposes the vendor, driver, and capability pickers", async () => {
    mount();
    fireEvent.click(screen.getByText("Acme Panel"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));
    // vendor + driver selects both offer the seeded registry rows
    expect(within(blade).getByRole("option", { name: "Crestron" })).toBeInTheDocument();
    expect(within(blade).getByRole("option", { name: "Crestron CT" })).toBeInTheDocument();
    // capability checkbox for the seeded capability
    expect(within(blade).getByText("Touchscreen")).toBeInTheDocument();
  });

  it("hides New product for a caller without product:create", () => {
    mount(viewer);
    expect(screen.queryByText(/New product/i)).toBeNull();
  });
});

// The catalog addresses rows by the kebab handle (ADR-0062): the first column
// shows it, and the substring filter matches it. With uuid-shaped fixture ids
// these fail when the page feeds the uuid anywhere an operator reads or types.
describe("Products addressing honesty (#469)", () => {
  afterEach(() => vi.restoreAllMocks());

  it("shows the handle in the Name column and finds the row by it in the filter", async () => {
    mount();
    expect(await screen.findByText("Acme Panel")).toBeInTheDocument();
    const cell = screen.getByText("Acme Panel").closest("td")!;
    expect(within(cell).getByText("acme-panel")).toBeInTheDocument();
    const input = screen.getByRole("combobox") as HTMLInputElement;
    fireEvent.input(input, { target: { value: "acme-panel" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(screen.getByText("Acme Panel")).toBeInTheDocument();
    expect(screen.queryByText("Crestron TSW-1070")).toBeNull();
  });
});

// The reference cells and the capability picker join on what the wire carries:
// vendor/driver cells show the kebab handle (not the uuid), and the picker
// compares capability NAMES (product.capabilities is a list of names).
describe("Products reference honesty (#470)", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders the vendor and driver handles in the catalog cells, never uuids", async () => {
    mount();
    expect(await screen.findByText("Crestron TSW-1070")).toBeInTheDocument();
    const row = screen.getByText("Crestron TSW-1070").closest("tr")!;
    expect(within(row).getByText("crestron")).toBeInTheDocument();
    expect(within(row).getByText("crestron-ct")).toBeInTheDocument();
    // No uuid leaks into the row.
    expect(within(row).queryByText(/00000000-0000-4000/)).toBeNull();
  });

  it("checks a product's current capabilities in the picker and can remove one", async () => {
    let sent: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PATCH" && req.url.includes("/products/acme-panel")) {
        sent = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({ ...seed[1], capabilities: [] }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      // Post-save invalidation refetch; any list shape satisfies the parsers.
      return new Response(JSON.stringify({ products: [], vendors: [], drivers: [], capabilities: [] }), { status: 200, headers: { "Content-Type": "application/json" } });
    });

    mount();
    fireEvent.click(await screen.findByText("Acme Panel"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));
    // The seeded capability arrives CHECKED (the picker joins on the name the
    // wire carries), and unchecking it actually removes it from the save body.
    const box = within(blade).getByLabelText(/Touchscreen/) as HTMLInputElement;
    expect(box.checked).toBe(true);
    fireEvent.click(box);
    expect(box.checked).toBe(false);
    fireEvent.click(within(blade).getByText("Save"));
    await waitFor(() => expect(sent).toBeTruthy());
    expect((sent as { capabilities: string[] }).capabilities).toEqual([]);
  });
});

// The catalog wears the shared identity cell (components/IdentityCell): one
// "Name" header whose cell carries the label over the handle, so a product reads
// the same here as it does on every other list. The separate "Display name"
// column went with it: the cell already renders both.
describe("Products identity column", () => {
  it("carries both identities under one Name header", async () => {
    mount();
    expect(await screen.findByText("Acme Panel")).toBeInTheDocument();
    const heads = screen.getAllByRole("columnheader").map((h) => h.textContent);
    expect(heads).toContain("Name");
    expect(heads).not.toContain("Display name");
    // Every other column survives, in order.
    expect(heads).toEqual(["Name", "Vendor", "Driver", "Kind", "Origin", ""]);

    const cell = screen.getByText("Acme Panel").closest("td")!;
    expect(within(cell).getByText("acme-panel")).toBeInTheDocument();
  });
});

// The create form leads with the display name and derives the handle from it, so
// an operator types "Acme Panel Pro" and never has to invent `acme-panel-pro` or
// think about the character class the API enforces. This proves the page is
// WIRED to the primitive; lib/entities.test.ts proves the suppression rule
// itself, which cannot be witnessed here (once a test types into an input its
// value property no longer tracks the signal).
describe("Products create identity", () => {
  const fields = async () => {
    mount();
    expect(await screen.findByText("Acme Panel")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /new product/i }));
    const display = screen.getByPlaceholderText("Crestron TSW-1070") as HTMLInputElement;
    const key = screen.getByPlaceholderText("crestron-tsw-1070") as HTMLInputElement;
    return { display, key };
  };

  it("derives the handle as the display name is typed", async () => {
    const { display, key } = await fields();
    fireEvent.input(display, { target: { value: "Acme Panel Pro" } });
    await waitFor(() => expect(key.value).toBe("acme-panel-pro"));
  });

  it("stops advertising the handle as derived once it is edited by hand", async () => {
    const { display, key } = await fields();
    fireEvent.input(display, { target: { value: "Acme Panel Pro" } });
    await waitFor(() => expect(key.value).toBe("acme-panel-pro"));

    fireEvent.input(key, { target: { value: "acme-panel-2" } });
    fireEvent.input(display, { target: { value: "Acme Panel Pro Mk2" } });

    await waitFor(() => expect(display.value).toBe("Acme Panel Pro Mk2"));
    expect(screen.getByText(/Globally unique address/)).toBeTruthy();
    expect(screen.queryByText(/Derived from the display name/)).toBeNull();
  });
});
