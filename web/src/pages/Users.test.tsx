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
  // The directory list (keyed on the show-archived flag, default false), and each
  // principal by id (the detail blade fetches getPrincipal so it resolves even an
  // archived user, hidden from the list).
  qc.setQueryData([...PRINCIPALS_KEY, false], seed);
  for (const pr of seed) qc.setQueryData([...PRINCIPALS_KEY, pr.id], pr);
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

  it("opens read-only, and the header pencil flips the profile to editable inputs and back", async () => {
    mount();
    fireEvent.click(screen.getByText("Alice Ng"));
    const blade = await waitFor(() => {
      const el = document.querySelector("aside[data-blade]");
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    // Read mode: the profile is facts, and there is no editable username input or Save.
    expect(within(blade).getByText("Username")).toBeTruthy();
    expect(within(blade).getByText("alice@example.com")).toBeTruthy();
    expect(within(blade).queryByLabelText("Username")).toBeNull();
    expect(within(blade).queryByText("Save")).toBeNull();
    // The header pencil opens edit mode: the username input is seeded, Save appears.
    fireEvent.click(within(blade).getByLabelText("Edit"));
    const input = (await within(blade).findByLabelText("Username")) as HTMLInputElement;
    expect(input.value).toBe("alice");
    expect(within(blade).getByText("Save")).toBeTruthy();
    expect(within(blade).getByText("Disable")).toBeTruthy(); // the destructive slot for a user
    // Cancel returns to read mode (input gone, pencil back).
    fireEvent.click(within(blade).getByText("Cancel"));
    expect(await within(blade).findByLabelText("Edit")).toBeTruthy();
    expect(within(blade).queryByLabelText("Username")).toBeNull();
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
      // POST creates carol; the detail blade fetches GET /principals/{id}; the list
      // is GET /principals. The detail resolves carol so her blade renders.
      const isDetailGet = method === "GET" && /\/principals\/[^/?]+$/.test(url);
      const body = url.includes("/principals") && method === "POST" ? carol : isDetailGet ? carol : { principals: [...seed, carol] };
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

  it("presents the live lifecycle in the footer: Disable in the left slot, Archive in the kebab, no Purge", async () => {
    mount();
    fireEvent.click(screen.getByText("Alice Ng"));
    const blade = await waitFor(() => {
      const el = document.querySelector("aside[data-blade]");
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    // Left slot: the reversible toggle.
    expect(within(blade).getByText("Disable")).toBeTruthy();
    // Kebab: the escalating soft delete, but not the hard delete (she is live).
    fireEvent.click(within(blade).getByLabelText("More actions"));
    expect(within(blade).getByText("Archive")).toBeTruthy();
    expect(within(blade).queryByText("Purge")).toBeNull();
  });

  it("an archived user (via Show archived) offers Restore in the slot and Purge in the kebab", async () => {
    const dana: Principal = { id: "u-dana", kind: "human", active: false, archived_at: "2026-01-01T00:00:00Z", human: { username: "dana", display_name: "Dana Vale" }, grants: [] };
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...PRINCIPALS_KEY, false], []); // default directory hides her
    qc.setQueryData([...PRINCIPALS_KEY, true], [dana]); // the "show archived" view
    qc.setQueryData([...PRINCIPALS_KEY, dana.id], dana);
    qc.setQueryData([...ME_KEY], me); // `>` grants purge (principal:purge:admin)
    qc.setQueryData([...ROLES_KEY], []);
    qc.setQueryData(["locations"], []);
    qc.setQueryData(["systems"], []);
    qc.setQueryData(["components"], []);
    render(() => (
      <QueryClientProvider client={qc}>
        <Router><Route path="*" component={() => <Users />} /></Router>
      </QueryClientProvider>
    ));
    // She is hidden until "Show archived" is on.
    expect(screen.queryByText("Dana Vale")).toBeNull();
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(await screen.findByText("Dana Vale"));
    const blade = await waitFor(() => {
      const el = document.querySelector("aside[data-blade]");
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    // Left slot restores; the kebab offers the irreversible purge.
    expect(within(blade).getByText("Restore")).toBeTruthy();
    fireEvent.click(within(blade).getByLabelText("More actions"));
    expect(within(blade).getByText("Purge")).toBeTruthy();
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
