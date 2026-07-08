import { describe, it, expect } from "vitest";
import { render, fireEvent, screen } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Roles from "./Roles";
import { ROLES_KEY, type Role } from "../lib/principals";

// The Roles page is now a config over the shared FlatList (the same list shell and
// blade detail as Users and Groups): a directory row per role, ordered by tier, and
// a read-only blade with the effective (flattened) permission grid. Data is seeded
// into the query cache so no server is needed.
const seed: Role[] = [
  { id: "owner", official: true, display_name: "Owner", description: "Full control, break-glass.", permissions: [">"], inherits: [], effective_permissions: [">"] },
  { id: "admin", official: true, display_name: "Administrator", description: "Manage the estate.", permissions: ["audit:read:admin"], inherits: ["operator"], effective_permissions: ["*:read", "principal:*", "audit:read:admin"] },
  { id: "operator", official: true, display_name: "Operator", description: "Day-to-day ops.", permissions: ["alarm:ack"], inherits: ["viewer"], effective_permissions: ["*:read", "alarm:ack"] },
  { id: "viewer", official: true, display_name: "Viewer", description: "Read only.", permissions: ["*:read"], inherits: [], effective_permissions: ["*:read"] },
];

function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ROLES_KEY], seed);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Roles />
    </QueryClientProvider>
  ));
}

describe("Roles page", () => {
  it("renders a directory row per role, ordered by tier (viewer before owner)", () => {
    mount();
    expect(screen.getByText("Owner")).toBeTruthy();
    expect(screen.getByText("Viewer")).toBeTruthy();
    expect(screen.getByText("Administrator")).toBeTruthy();
    const body = document.body.textContent ?? "";
    expect(body.indexOf("Viewer")).toBeLessThan(body.indexOf("Owner"));
  });

  it("opens a read-only blade with the effective permission grid and description", async () => {
    mount();
    fireEvent.click(screen.getByText("Viewer"));
    expect(await screen.findByText("Read only.")).toBeTruthy();
    // *:read renders a `read` action chip in the grid.
    expect(screen.getAllByText("read").length).toBeGreaterThan(0);
  });

  it("shows the > superuser as an 'everything' chip in the owner blade", async () => {
    mount();
    fireEvent.click(screen.getByText("Owner"));
    expect(await screen.findByText("everything")).toBeTruthy();
  });

  it("shows what a role inherits in its row", () => {
    mount();
    // viewer appears as its own id badge AND in operator's Inherits cell.
    expect(screen.getAllByText("viewer").length).toBeGreaterThanOrEqual(2);
  });

  it("wraps the catalog in the shared ListShell filter bar", () => {
    mount();
    expect(screen.getByRole("combobox")).toBeTruthy();
    expect(screen.getByPlaceholderText("filter roles by name or permission")).toBeTruthy();
  });
});
