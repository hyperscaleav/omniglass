import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Drivers from "./Drivers";
import { DRIVERS_KEY, type Driver } from "../lib/drivers";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// Drivers is the driver registry on the flat FlatList surface (the driver
// picker on the product form). Data is seeded into the query cache so no
// server is needed. Registry rows carry a uuid id and the kebab handle in
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

// The catalog addresses rows by the kebab handle (ADR-0062): the first column
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
