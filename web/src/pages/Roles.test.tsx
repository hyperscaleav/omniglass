import { describe, it, expect } from "vitest";
import { render } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Roles from "./Roles";
import { ROLES_KEY, type Role } from "../lib/principals";

// The Roles page is a read-only, self-teaching catalog: it renders each role's
// display name, description, inheritance, and effective (flattened) permissions,
// ordered by tier. Data is seeded into the query cache so no server is needed.
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
  it("renders each role's display name, description, and effective permissions", () => {
    const { getByText, getAllByText } = mount();
    expect(getByText("Owner")).toBeTruthy();
    expect(getByText("Full control, break-glass.")).toBeTruthy();
    expect(getByText("Read only.")).toBeTruthy();
    // effective permission chips are grouped per resource; the actions render.
    expect(getAllByText("read").length).toBeGreaterThan(0); // *:read on viewer + operator
    expect(getByText("ack")).toBeTruthy(); // operator's effective alarm:ack
  });

  it("renders the > superuser as an 'everything' chip and marks an :admin permission", () => {
    const { getByText } = mount();
    expect(getByText("everything")).toBeTruthy(); // owner's `>`
    expect(getByText(":admin")).toBeTruthy(); // admin's audit:read:admin tier marker
  });

  it("orders roles by tier, least powerful first (viewer, operator, admin, owner)", () => {
    const { getAllByRole } = mount();
    const headings = getAllByRole("heading", { level: 2 }).map((h) => h.textContent);
    expect(headings).toEqual(["Viewer", "Operator", "Administrator", "Owner"]);
  });

  it("shows what a role inherits", () => {
    const { getAllByText } = mount();
    expect(getAllByText(/inherits/).length).toBeGreaterThan(0);
    // "viewer" appears as its own id badge AND in operator's inheritance.
    expect(getAllByText("viewer").length).toBeGreaterThanOrEqual(2);
  });
});
