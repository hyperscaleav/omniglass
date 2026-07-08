import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Users from "./Users";
import { PRINCIPALS_KEY, ROLES_KEY, type Principal } from "../lib/principals";
import { GROUPS_KEY } from "../lib/groups";
import { ME_KEY, type Me } from "../lib/auth";

// The Users page is a config over the shared FlatList: a directory row per
// principal, a row opening the detail blade (facts, the groups it belongs to, grants,
// disable / enable, impersonate, and an inline edit), and a create Drawer. Data is
// seeded into the query cache so no server is needed; `>` grants the caller every
// permission, so every gated affordance is present.
const seed: Principal[] = [
  { id: "u-alice", kind: "human", active: true, human: { username: "alice", email: "alice@example.com", display_name: "Alice Ng" }, grants: [{ id: "g1", role: "admin", scope_kind: "all" }], groups: [{ id: "g-hd", name: "Help Desk" }] },
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
  // The group a user drills into (its blade self-fetches these by id).
  qc.setQueryData([...GROUPS_KEY, "g-hd"], { id: "g-hd", name: "help-desk", display_name: "Help Desk" });
  qc.setQueryData([...GROUPS_KEY, "g-hd", "members"], []);
  qc.setQueryData([...GROUPS_KEY, "g-hd", "grants"], []);
  // A Router wraps the page so its router hooks (the ?u= deep-link uses
  // useSearchParams) have a context; a catch-all route renders Users.
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={() => <Users />} />
      </Router>
    </QueryClientProvider>
  ));
}

describe("Users page", () => {
  afterEach(() => vi.restoreAllMocks());

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

  it("lands on the new user's detail Drawer after a successful create", async () => {
    // The create POST returns the new principal; the directory refetch (triggered
    // by the invalidate) returns it alongside the seed, so the detail Drawer,
    // which re-derives by id from the live query, resolves the fresh row.
    const carol: Principal = { id: "u-carol", kind: "human", active: true, human: { username: "carol", email: "carol@example.com", display_name: "Carol Diaz" }, grants: [] };
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const url = typeof input === "string" ? input : req.url;
      const method = typeof input === "string" ? "GET" : req.method;
      const body = url.includes("/principals") && method === "POST" ? carol : { principals: [...seed, carol] };
      return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
    });

    mount();
    fireEvent.click(screen.getByText("New user"));
    const username = (await screen.findByLabelText("Username")) as HTMLInputElement;
    fireEvent.input(username, { target: { value: "carol" } });
    fireEvent.click(screen.getByText("Create user"));
    // The create Drawer gives way to the new user's detail Drawer: its email fact
    // (only present in the detail view) shows, and the create submit is gone.
    expect(await screen.findByText("carol@example.com")).toBeTruthy();
    await waitFor(() => expect(screen.queryByText("Create user")).toBeNull());
  });

  it("drills from a user's group to a group blade nested over the user (user -> group)", async () => {
    mount();
    fireEvent.click(screen.getByText("Alice Ng"));
    // The user blade lists the groups the user belongs to.
    const userBlade = await waitFor(() => {
      const el = document.querySelector("aside[data-blade]");
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    // Click the group inside the blade (not the list badge behind it): a second
    // blade (the group) opens over the user.
    fireEvent.click(within(userBlade).getByText("Help Desk"));
    await waitFor(() => expect(document.querySelectorAll("aside[data-blade]").length).toBe(2));
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
