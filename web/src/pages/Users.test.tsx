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

  it("lands on the new user's detail in edit mode after a successful create", async () => {
    // The create POST returns the new principal; the create flow seeds its detail
    // cache and flags it to open in edit mode, so the blade opens on carol's seeded,
    // editable fields (add grants right away) rather than a read-only view.
    const carol: Principal = { id: "u-carol", kind: "human", active: true, human: { username: "carol", email: "carol@example.com", display_name: "Carol Diaz" }, grants: [] };
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const url = typeof input === "string" ? input : req.url;
      const method = typeof input === "string" ? "GET" : req.method;
      const isDetailGet = method === "GET" && /\/principals\/[^/?]+$/.test(url);
      const body = url.includes("/principals") && method === "POST" ? carol : isDetailGet ? carol : { principals: [...seed, carol] };
      return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
    });

    mount();
    fireEvent.click(screen.getByText("New user"));
    const username = (await screen.findByLabelText("Username")) as HTMLInputElement;
    fireEvent.input(username, { target: { value: "carol" } });
    fireEvent.click(screen.getByText("Create user"));
    // The create Drawer gives way to carol's blade, already in edit mode: the Save
    // button is present and her email is a seeded input (not a read-only fact).
    await waitFor(() => expect(screen.queryByText("Create user")).toBeNull());
    const blade = await waitFor(() => {
      const el = document.querySelector("aside[data-blade]");
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect(await within(blade).findByText("Save")).toBeTruthy();
    expect((within(blade).getByLabelText("Email") as HTMLInputElement).value).toBe("carol@example.com");
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

  it("archiving a user closes the blade", async () => {
    // Archive hides the user from the directory, so its blade closes (the account
    // still exists and is restorable, unlike a purge). The POST 204s and the refetch
    // returns the directory without her.
    vi.spyOn(window, "confirm").mockReturnValue(true);
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const url = typeof input === "string" ? input : req.url;
      if (url.includes(":archive")) return new Response(null, { status: 204 });
      return new Response(JSON.stringify({ principals: [] }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount();
    fireEvent.click(screen.getByText("Alice Ng"));
    const blade = await waitFor(() => {
      const el = document.querySelector("aside[data-blade]");
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("More actions"));
    fireEvent.click(within(blade).getByText("Archive"));
    await waitFor(() => expect(document.querySelector("aside[data-blade]")).toBeNull());
  });

  it("purging an archived user closes the blade and does not refetch the dead detail", async () => {
    const dana: Principal = { id: "u-dana", kind: "human", active: false, archived_at: "2026-01-01T00:00:00Z", human: { username: "dana", display_name: "Dana Vale" }, grants: [] };
    // Confirm the purge; the POST 204s, the directory refetch returns an empty list,
    // and a GET of the purged principal's own detail 404s (as the real server would).
    // The blade must close, and the now-dead detail query must not be refetched (an
    // orphan 404 that would keep the blade in a broken state).
    let detailRefetched = false;
    vi.spyOn(window, "confirm").mockReturnValue(true);
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const url = typeof input === "string" ? input : req.url;
      const method = typeof input === "string" ? "GET" : req.method;
      if (url.includes(":purge")) return new Response(null, { status: 204 });
      if (method === "GET" && /\/principals\/u-dana(\?|$)/.test(url)) {
        detailRefetched = true;
        return new Response(JSON.stringify({ title: "not found" }), { status: 404, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ principals: [] }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...PRINCIPALS_KEY, false], []);
    qc.setQueryData([...PRINCIPALS_KEY, true], [dana]);
    qc.setQueryData([...PRINCIPALS_KEY, dana.id], dana);
    qc.setQueryData([...ME_KEY], me);
    qc.setQueryData([...ROLES_KEY], []);
    qc.setQueryData(["locations"], []);
    qc.setQueryData(["systems"], []);
    qc.setQueryData(["components"], []);
    render(() => (
      <QueryClientProvider client={qc}>
        <Router><Route path="*" component={() => <Users />} /></Router>
      </QueryClientProvider>
    ));
    fireEvent.click(screen.getByRole("checkbox")); // show archived
    fireEvent.click(await screen.findByText("Dana Vale"));
    const blade = await waitFor(() => {
      const el = document.querySelector("aside[data-blade]");
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("More actions"));
    fireEvent.click(within(blade).getByText("Purge"));
    // The blade closes: no aside remains.
    await waitFor(() => expect(document.querySelector("aside[data-blade]")).toBeNull());
    // And the purged principal's own detail query was not refetched (no orphan 404).
    expect(detailRefetched).toBe(false);
  });

  it("disables Create and shows an inline error for an invalid username", async () => {
    mount();
    fireEvent.click(screen.getByText("New user"));
    const username = (await screen.findByLabelText("Username")) as HTMLInputElement;
    const createBtn = () => screen.getByText("Create user").closest("button") as HTMLButtonElement;
    // Capitals and spaces are refused inline: an error shows and Create is disabled.
    fireEvent.input(username, { target: { value: "Jordan Smith" } });
    expect(screen.getByText(/space|capital/i)).toBeTruthy();
    expect(createBtn().disabled).toBe(true);
    // A valid lowercase handle clears the error and enables Create.
    fireEvent.input(username, { target: { value: "jordan" } });
    expect(screen.queryByText(/space|capital/i)).toBeNull();
    expect(createBtn().disabled).toBe(false);
  });

  it("generates a strong initial password and enables Create; a weak one blocks it", async () => {
    mount();
    fireEvent.click(screen.getByText("New user"));
    const username = (await screen.findByLabelText("Username")) as HTMLInputElement;
    fireEvent.input(username, { target: { value: "jordan" } });
    const pw = screen.getByLabelText("Initial password") as HTMLInputElement;
    const createBtn = () => screen.getByText("Create user").closest("button") as HTMLButtonElement;
    // A weak manual password: inline error (distinct from the helper text) + disabled Create.
    fireEvent.input(pw, { target: { value: "abc" } });
    expect(screen.getByText(/use at least/i)).toBeTruthy();
    expect(createBtn().disabled).toBe(true);
    // Generate fills a strong password, kept masked, clears the error, enables Create.
    fireEvent.click(screen.getByRole("button", { name: "Generate" }));
    expect(pw.value.length).toBeGreaterThanOrEqual(12);
    expect(pw.type).toBe("password");
    expect(screen.queryByText(/use at least/i)).toBeNull();
    expect(createBtn().disabled).toBe(false);
  });

  it("renders a server password-policy rejection inline under the field, not as a form alert", async () => {
    // A common password passes the inline length check but the server denylist refuses
    // it (422). The message should read like the other inline errors, under the field.
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const url = typeof input === "string" ? input : req.url;
      const method = typeof input === "string" ? "GET" : req.method;
      if (method === "POST" && url.includes("/principals")) {
        return new Response(JSON.stringify({ detail: "password is too common; choose a less predictable one" }), { status: 422, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ principals: seed }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount();
    fireEvent.click(screen.getByText("New user"));
    fireEvent.input(await screen.findByLabelText("Username"), { target: { value: "commonpw" } });
    fireEvent.input(screen.getByLabelText("Initial password"), { target: { value: "administrator" } });
    fireEvent.click(screen.getByText("Create user"));
    // The policy message shows (inline), and there is no head-of-form alert carrying it.
    expect(await screen.findByText(/too common/i)).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("disables the footer Save when an edited username is invalid", async () => {
    mount();
    fireEvent.click(screen.getByText("Alice Ng"));
    const blade = await waitFor(() => {
      const el = document.querySelector("aside[data-blade]");
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));
    const username = (await within(blade).findByLabelText("Username")) as HTMLInputElement;
    const saveBtn = () => within(blade).getByText("Save").closest("button") as HTMLButtonElement;
    expect(saveBtn().disabled).toBe(false);
    fireEvent.input(username, { target: { value: "Alice Ng" } }); // caps + space
    expect(within(blade).getByText(/space|capital/i)).toBeTruthy();
    expect(saveBtn().disabled).toBe(true);
    fireEvent.input(username, { target: { value: "alice" } });
    expect(saveBtn().disabled).toBe(false);
  });

  it("resets a user's password from the kebab and confirms", async () => {
    let resetCalled = false;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const url = typeof input === "string" ? input : req.url;
      const method = typeof input === "string" ? "GET" : req.method;
      if (method === "POST" && url.includes(":resetPassword")) {
        resetCalled = true;
        return new Response(null, { status: 204 });
      }
      return new Response(JSON.stringify({ principals: seed }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount();
    fireEvent.click(screen.getByText("Alice Ng"));
    const blade = await waitFor(() => {
      const el = document.querySelector("aside[data-blade]");
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    // Reset password is in the kebab; it opens an inline panel with its own field.
    fireEvent.click(within(blade).getByLabelText("More actions"));
    fireEvent.click(within(blade).getByText("Reset password"));
    fireEvent.click(within(blade).getByRole("button", { name: "Generate" }));
    fireEvent.click(within(blade).getByText("Set password"));
    await waitFor(() => expect(resetCalled).toBe(true));
    expect(await within(blade).findByText(/password set/i)).toBeTruthy();
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
