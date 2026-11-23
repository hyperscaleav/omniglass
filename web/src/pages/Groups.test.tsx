import { describe, it, expect, afterEach, vi } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Groups from "./Groups";
import { GROUPS_KEY, type Group, type GroupMapping, type GroupMember } from "../lib/groups";
import { USERS_KEY, ROLES_KEY, type User } from "../lib/users";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// The Groups page is a config over the shared FlatList (rooted on group): a row per
// group opens the group blade (members drill into the member's user blade), and the
// per-group caches are seeded so no server is needed. `>` grants every permission.
const group: Group = { id: uuidFor("g-hd"), name: "help-desk", label: "Help Desk", description: "Support crew", member_count: 1, grant_count: 0 };
const members: GroupMember[] = [{ user_id: uuidFor("u-alice"), kind: "person", name: "alice" }];
const alice: User = { id: uuidFor("u-alice"), kind: "person", active: true, person: { username: "alice", email: "alice@example.com", label: "Alice Ng" }, grants: [], groups: [{ id: uuidFor("g-hd"), name: "Help Desk" }] };
const me: Me = { user: { id: "u-root", kind: "person" }, person: { username: "root" }, permissions: [">"], grants: [] };

function mount(opts: { members?: GroupMember[]; mappings?: GroupMapping[] } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...GROUPS_KEY, uuidFor("g-hd"), "idp-mappings"], opts.mappings ?? []);
  qc.setQueryData([...GROUPS_KEY], [group]);
  qc.setQueryData([...GROUPS_KEY, uuidFor("g-hd")], group);
  qc.setQueryData([...GROUPS_KEY, uuidFor("g-hd"), "members"], opts.members ?? members);
  qc.setQueryData([...GROUPS_KEY, uuidFor("g-hd"), "grants"], []);
  qc.setQueryData([...USERS_KEY], [alice]);
  qc.setQueryData([...USERS_KEY, alice.id], alice); // the drilled user blade fetches getUser by id
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...ROLES_KEY], []);
  qc.setQueryData(["locations"], []);
  qc.setQueryData(["systems"], []);
  qc.setQueryData(["components"], []);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={() => <Groups />} />
      </Router>
    </QueryClientProvider>
  ));
}

const asides = () => document.querySelectorAll("aside[data-blade]");

describe("Groups page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders a directory row per group with its member and grant counts", () => {
    mount();
    expect(screen.getByText("Help Desk")).toBeTruthy();
    expect(screen.getByText("help-desk")).toBeTruthy(); // the name beneath the label
    expect(screen.getByText("Support crew")).toBeTruthy();
  });

  it("shows a group's name once when it carries no label", () => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...GROUPS_KEY], [{ id: uuidFor("g-bare"), name: "night-ops" } as Group]);
    qc.setQueryData([...ME_KEY], me);
    render(() => (
      <QueryClientProvider client={qc}>
        <Router>
          <Route path="*" component={() => <Groups />} />
        </Router>
      </QueryClientProvider>
    ));
    // The label IS the name, so rendering it twice would be noise.
    expect(screen.getAllByText("night-ops")).toHaveLength(1);
  });

  it("opens read-only with a footer bar; Delete is always there, Edit reveals the member controls", async () => {
    mount();
    fireEvent.click(screen.getByText("Help Desk"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    // Read mode footer: Delete is always available (not gated by edit); Edit present;
    // but the body has no member Remove and no Save yet.
    expect(within(blade).getByText("Delete user group")).toBeTruthy();
    expect(within(blade).getByLabelText("Edit")).toBeTruthy();
    expect(within(blade).queryByLabelText("Remove")).toBeNull();
    expect(within(blade).queryByText("Save")).toBeNull();
    // Edit reveals the member controls and swaps the right cluster to Cancel / Save.
    fireEvent.click(within(blade).getByLabelText("Edit"));
    expect(within(blade).getByText("Save")).toBeTruthy();
    expect(within(blade).getByText("Cancel")).toBeTruthy();
    expect(within(blade).getAllByLabelText("Remove").length).toBeGreaterThan(0);
    expect(within(blade).getByText("Delete user group")).toBeTruthy(); // still there in edit
  });

  it("stages a member removal in edit mode and Cancel reverts it", async () => {
    mount();
    fireEvent.click(screen.getByText("Help Desk"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));
    expect(within(blade).getByText("alice")).toBeTruthy();
    // Staging a removal drops the member from the effective list (not yet committed).
    fireEvent.click(within(blade).getAllByLabelText("Remove")[0]);
    expect(within(blade).queryByText("alice")).toBeNull();
    // Cancel reverts the staging and returns to read mode (Save gone, member back).
    fireEvent.click(within(blade).getByText("Cancel"));
    expect(within(blade).getByText("alice")).toBeTruthy();
    expect(within(blade).queryByText("Save")).toBeNull();
  });

  it("deleting a group closes the blade and does not refetch the dead detail", async () => {
    // The DELETE 204s and the directory refetch returns an empty list; a GET of the
    // deleted group's own detail 404s (as the real server would). The blade must
    // close, and the dead detail query must not be refetched (an orphan 404).
    let detailRefetched = false;
    vi.spyOn(window, "confirm").mockReturnValue(true);
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const url = typeof input === "string" ? input : req.url;
      const method = typeof input === "string" ? "GET" : req.method;
      if (method === "DELETE") return new Response(null, { status: 204 });
      if (method === "GET" && url.includes(`/user-groups/${uuidFor("g-hd")}`)) {
        detailRefetched = true;
        return new Response(JSON.stringify({ title: "not found" }), { status: 404, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ groups: [] }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount();
    fireEvent.click(screen.getByText("Help Desk"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByText("Delete user group"));
    await waitFor(() => expect(asides().length).toBe(0));
    expect(detailRefetched).toBe(false);
  });

  it("disables Create and shows an inline error for an invalid group name", async () => {
    mount();
    fireEvent.click(screen.getByText("New user group"));
    const name = (await screen.findByPlaceholderText("field-crew")) as HTMLInputElement;
    const createBtn = () => screen.getByText("Create user group").closest("button") as HTMLButtonElement;
    fireEvent.input(name, { target: { value: "Field Crew" } }); // caps + space
    expect(screen.getByText(/space|capital/i)).toBeTruthy();
    expect(createBtn().disabled).toBe(true);
    fireEvent.input(name, { target: { value: "field-crew" } });
    expect(screen.queryByText(/space|capital/i)).toBeNull();
    expect(createBtn().disabled).toBe(false);
  });

  it("opens a newly created group straight in edit mode to add members and grants", async () => {
    const created: Group = { id: uuidFor("g-new"), name: "field-crew", label: "Field Crew", description: "" };
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const url = typeof input === "string" ? input : req.url;
      const method = typeof input === "string" ? "GET" : req.method;
      if (method === "POST" && url.includes("/user-groups")) {
        return new Response(JSON.stringify(created), { status: 201, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ groups: [created] }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount();
    fireEvent.click(screen.getByText("New user group"));
    const name = (await screen.findByPlaceholderText("field-crew")) as HTMLInputElement;
    fireEvent.input(name, { target: { value: "field-crew" } });
    fireEvent.click(screen.getByText("Create user group"));
    // The new group's blade opens already in edit mode: Save / Cancel and the
    // edit-only member add control are present without clicking Edit.
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect(await within(blade).findByText("Save")).toBeTruthy();
    expect(within(blade).getByText("Cancel")).toBeTruthy();
    expect(within(blade).getByText("Add a member...")).toBeTruthy(); // the member add picker (edit-only)
  });

  it("drills from a group member to a user blade nested over the group (group -> user)", async () => {
    mount();
    fireEvent.click(screen.getByText("Help Desk")); // open the group blade
    const groupBlade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    // The member renders in the group blade; clicking it opens the user blade over it.
    fireEvent.click(within(groupBlade).getByText("alice"));
    await waitFor(() => expect(asides().length).toBe(2));
  });

  it("makes the user blade terminal (its groups do not drill back) so depth stays bounded", async () => {
    mount();
    fireEvent.click(screen.getByText("Help Desk"));
    await waitFor(() => expect(asides().length).toBe(1));
    fireEvent.click(within(asides()[0] as HTMLElement).getByText("alice"));
    await waitFor(() => expect(asides().length).toBe(2));
    // The user blade (group is the root) shows the user's groups read-only: clicking
    // one does not open a third blade.
    const userBlade = asides()[1] as HTMLElement;
    fireEvent.click(within(userBlade).getByText("Help Desk"));
    expect(asides().length).toBe(2);
  });
});

// Identity provider groups (#2363): the roster says which members single
// sign-on brought, and the mapping editor stages claim values beside the
// members, applied on Save.
describe("Groups identity provider mappings", () => {
  afterEach(() => vi.restoreAllMocks());
  const issuer = "https://login.example.com/tenant";
  const openBlade = async () => {
    fireEvent.click(screen.getByText("Help Desk"));
    return waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
  };

  it("marks a member single sign-on brought, naming the provider's host", async () => {
    mount({ members: [...members, { user_id: uuidFor("u-bob"), kind: "person", name: "bob", source: "idp", issuer }] });
    const blade = await openBlade();
    expect(within(blade).getByText("from SSO (login.example.com)")).toBeTruthy();
    expect(within(blade).getAllByText(/^from SSO/)).toHaveLength(1); // alice is local
  });

  it("lists the mapped claim values and stages a new one, posted on Save", async () => {
    const posted: unknown[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const req = input instanceof Request ? input : new Request(input, init);
      if (req.method === "POST" && req.url.includes("/idp-mappings")) {
        posted.push(await req.json());
        return new Response(JSON.stringify({ id: uuidFor("m-new"), group_id: group.id, group: group.name, issuer, claim_value: "night-ops", created_at: "2026-11-23T00:00:00Z" }), { status: 201, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({}), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount({ mappings: [{ id: uuidFor("m-av"), group_id: group.id, issuer, claim_value: "av-support" }] });
    const blade = await openBlade();
    expect(within(blade).getByText("av-support")).toBeTruthy();
    expect(within(blade).queryByPlaceholderText(/groups claim value/)).toBeNull(); // edit-only
    fireEvent.click(within(blade).getByLabelText("Edit"));
    const entry = within(blade).getByPlaceholderText(/groups claim value/) as HTMLInputElement;
    fireEvent.input(entry, { target: { value: "night-ops" } });
    fireEvent.keyDown(entry, { key: "Enter" });
    expect(within(blade).getByText("night-ops")).toBeTruthy();
    expect(posted).toHaveLength(0); // staged, not sent
    fireEvent.click(within(blade).getByText("Save"));
    await waitFor(() => expect(posted).toEqual([{ claim_value: "night-ops" }]));
  });
});

// The create form leads with the label and derives the name from it, so an
// admin types "Field Crew" and never has to invent `field-crew`. These two prove
// the page is WIRED to lib/entities; the suppression rule itself (a hand-edited
// name stops following) is asserted in lib/entities.test.ts, because once a test
// types into an input its DOM value stops tracking the signal.
describe("Groups create identity", () => {
  const openCreate = async () => {
    mount();
    fireEvent.click(screen.getByText("New user group"));
    const display = (await screen.findByPlaceholderText("Field Crew")) as HTMLInputElement;
    return { display, name: screen.getByPlaceholderText("field-crew") as HTMLInputElement };
  };

  it("derives the name as the label is typed", async () => {
    const { display, name } = await openCreate();
    fireEvent.input(display, { target: { value: "Field Crew" } });
    await waitFor(() => expect(name.value).toBe("field-crew"));
  });

  it("stops advertising the name as derived once it is edited by hand", async () => {
    const { display, name } = await openCreate();
    fireEvent.input(display, { target: { value: "Field Crew" } });
    await waitFor(() => expect(name.value).toBe("field-crew"));

    fireEvent.input(name, { target: { value: "crew-west" } });
    expect(screen.getByText(/Globally unique address/)).toBeTruthy();
    expect(screen.queryByText(/Derived from the label/)).toBeNull();
  });
});

// The blade's edit mode composes with its id deep link (#762): ?g=<id>&edit=1
// opens the group's blade already editing, which is also how the create flow
// hands off (the one-shot openGroupInEdit signal is gone).
describe("edit as a URL fact", () => {
  afterEach(() => window.history.pushState({}, "", "/"));

  it("opens the group's blade in edit when the URL carries ?g=<id>&edit=1", async () => {
    window.history.pushState({}, "", `/?g=${uuidFor("g-hd")}&edit=1`);
    mount();
    // The blade footer's edit pair is the witness: Save renders only in edit mode.
    await waitFor(() => expect(screen.getByText("Save")).toBeTruthy());
  });
});
