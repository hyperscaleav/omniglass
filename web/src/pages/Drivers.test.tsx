import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Drivers from "./Drivers";
import { DRIVERS_KEY, type Driver } from "../lib/drivers";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// Drivers is the driver registry on the flat FlatList surface (the driver
// picker on the product form). Data is seeded into the query cache so no
// server is needed. Registry rows carry a uuid id and the name in
// name (ADR-0062); the console addresses rows by the handle.
const seed: Driver[] = [
  { id: uuidFor("drv-snmp-generic"), name: "snmp-generic", display_name: "Generic SNMP", official: true, version: "1.0.0" },
  { id: uuidFor("drv-acme-driver"), name: "acme-driver", display_name: "Acme Driver", official: false },
];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...DRIVERS_KEY], seed);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Drivers />
    </QueryClientProvider>
  ));
}

// The catalog addresses rows by the name (ADR-0062): the first column
// shows it, and the substring filter matches it. With uuid-shaped fixture ids
// these fail when the page feeds the uuid anywhere an operator reads or types.
describe("Drivers addressing honesty (#469)", () => {
  afterEach(() => vi.restoreAllMocks());

  it("shows the handle in the Name column and finds the row by it in the filter", async () => {
    mount();
    expect(await screen.findByText("Generic SNMP")).toBeInTheDocument();
    const row = screen.getByText("Generic SNMP").closest("tr")!;
    expect(within(row).getByText("snmp-generic")).toBeInTheDocument();
    const input = screen.getByRole("combobox") as HTMLInputElement;
    fireEvent.input(input, { target: { value: "snmp-generic" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(screen.getByText("Generic SNMP")).toBeInTheDocument();
    expect(screen.queryByText("Acme Driver")).toBeNull();
  });
});

// The identity primitive: one "Name" column carrying the label over the handle,
// not a pair of columns that read differently here than on the next page.
describe("Drivers identity column", () => {
  afterEach(() => vi.restoreAllMocks());

  it("carries both identities in one Name column", async () => {
    mount();
    await screen.findByText("Generic SNMP");
    expect(screen.getByRole("columnheader", { name: "Name" })).toBeInTheDocument();
    expect(screen.queryByRole("columnheader", { name: /display name/i })).toBeNull();
  });
});

// The create form's two identity fields are coupled: an operator types the label
// and the handle follows, which is the only way the kebab constraint stays out of
// their way. Hand-editing the handle ends the coupling for good.
describe("Drivers create derives the handle", () => {
  afterEach(() => vi.restoreAllMocks());

  it("derives the handle from the display name until the operator edits it", async () => {
    mount();
    fireEvent.click(await screen.findByRole("button", { name: /new driver/i }));
    const display = screen.getByPlaceholderText("Generic SNMP") as HTMLInputElement;
    const handle = screen.getByPlaceholderText("snmp-generic") as HTMLInputElement;

    fireEvent.input(display, { target: { value: "Acme PTZ Camera" } });
    expect(handle.value).toBe("acme-ptz-camera");
    expect(screen.getByText(/derived from the display name/i)).toBeInTheDocument();

    fireEvent.input(handle, { target: { value: "acme-ptz" } });
    fireEvent.input(display, { target: { value: "Acme PTZ Camera v2" } });
    expect(handle.value).toBe("acme-ptz");
    // The field stops advertising a coupling it no longer has.
    expect(screen.queryByText(/derived from the display name/i)).toBeNull();
  });
});

// A successful create lands the operator on the new row (#471): FlatList's
// create ctx exposes select for exactly this, but every catalog page wired
// onCreated to close, silently discarding the created row.
describe("Drivers create lands on the new row (#471)", () => {
  afterEach(() => vi.restoreAllMocks());

  it("opens the created driver's blade after a successful create", async () => {
    const created = { id: uuidFor("drv-custom-x"), name: "custom-x", display_name: "Custom X", official: false };
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST" && req.url.includes("/drivers")) {
        return new Response(JSON.stringify(created), { status: 201, headers: { "Content-Type": "application/json" } });
      }
      // The post-create invalidation refetch sees the new row.
      return new Response(JSON.stringify({ drivers: [...seed, created] }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount();
    fireEvent.click(await screen.findByRole("button", { name: /new driver/i }));
    fireEvent.input(screen.getByPlaceholderText("snmp-generic"), { target: { value: "custom-x" } });
    fireEvent.input(screen.getByPlaceholderText("Generic SNMP"), { target: { value: "Custom X" } });
    fireEvent.click(screen.getByRole("button", { name: /create driver/i }));
    const blade = await waitFor(() => {
      const el = document.querySelector("aside[data-blade]");
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    // The handle shows in both the blade title and its Name row.
    expect(within(blade).getAllByText("custom-x").length).toBeGreaterThan(0);
  });
});
