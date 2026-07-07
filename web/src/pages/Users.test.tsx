import { describe, it, expect } from "vitest";
import { render, fireEvent, screen } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Users from "./Users";
import { PRINCIPALS_KEY, ROLES_KEY, type Principal } from "../lib/principals";
import { ME_KEY, type Me } from "../lib/auth";

// The Users page is a config over the shared FlatList: a directory row per
// principal, a row opening the side Drawer detail (facts, grants, disable / enable,
// impersonate, and an inline edit), and a create Drawer. Data is seeded into the
// query cache so no server is needed; `>` grants the caller every permission, so
// every gated affordance is present.
const seed: Principal[] = [
  { id: "u-alice", kind: "human", active: true, human: { username: "alice", email: "alice@example.com", display_name: "Alice Ng" }, grants: [{ id: "g1", role: "admin", scope_kind: "all" }] },
  { id: "u-svc", kind: "service", active: true, service: { label: "ingest-bot" }, grants: [] },
  { id: "u-bob", kind: "human", active: false, human: { username: "bob" }, grants: [] },
];

const me: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };

function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...PRINCIPALS_KEY], seed);
  qc.setQueryData([...ME_KEY], me);
  // The grant builder's catalogs; empty is enough for the detail Drawer to render.
  qc.setQueryData([...ROLES_KEY], []);
  qc.setQueryData(["locations"], []);
  qc.setQueryData(["systems"], []);
  qc.setQueryData(["components"], []);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Users />
    </QueryClientProvider>
  ));
}

describe("Users page", () => {
  it("renders a directory row per principal: name, username, kind, and grant count", () => {
    const { getByText, getAllByText } = mount();
    expect(getByText("Alice Ng")).toBeTruthy();
    expect(getByText("alice")).toBeTruthy(); // the username under the display name
    expect(getByText("ingest-bot")).toBeTruthy(); // service principal's label as its name
    // Both a human and a service badge render (capitalized via CSS, text is the kind).
    expect(getAllByText("human").length).toBeGreaterThan(0);
    expect(getByText("service")).toBeTruthy();
    expect(getByText("inactive")).toBeTruthy(); // bob is disabled
  });

  it("opens the detail Drawer on a row and swaps to the inline edit form and back", async () => {
    mount();
    fireEvent.click(screen.getByText("Alice Ng"));
    // The Drawer detail shows the profile facts and the admin affordances.
    expect(await screen.findByText("Username")).toBeTruthy();
    expect(screen.getByText("alice@example.com")).toBeTruthy();
    const edit = screen.getByText("Edit");
    expect(edit).toBeTruthy();
    // Edit swaps the read view to the inline edit form (no nested dialog): the
    // username input is seeded with the current value.
    fireEvent.click(edit);
    const input = (await screen.findByLabelText("Username")) as HTMLInputElement;
    expect(input.value).toBe("alice");
    expect(screen.getByText("Save changes")).toBeTruthy();
    // Cancel returns to the read view (the Edit button is back, the form is gone).
    fireEvent.click(screen.getByText("Cancel"));
    expect(await screen.findByText("Edit")).toBeTruthy();
    expect(screen.queryByText("Save changes")).toBeNull();
  });

  it("narrows the directory through the FilterBar (kind:service keeps only the bot)", async () => {
    const { queryByText } = mount();
    const input = screen.getByRole("combobox") as HTMLInputElement;
    fireEvent.input(input, { target: { value: "kind:service" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(screen.getByText("ingest-bot")).toBeTruthy();
    expect(queryByText("Alice Ng")).toBeNull();
  });
});
