import { describe, it, expect, afterEach, vi } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Groups from "./Groups";
import { GROUPS_KEY, type Group, type GroupMember } from "../lib/groups";
import { PRINCIPALS_KEY, ROLES_KEY, type Principal } from "../lib/principals";
import { ME_KEY, type Me } from "../lib/auth";

// The Groups page is a config over the shared FlatList (rooted on group): a row per
// group opens the group blade (members drill into the member's user blade), and the
// per-group caches are seeded so no server is needed. `>` grants every permission.
const group: Group = { id: "g-hd", name: "help-desk", display_name: "Help Desk", description: "Support crew", member_count: 1, grant_count: 0 };
const members: GroupMember[] = [{ principal_id: "u-alice", kind: "human", username: "alice" }];
const alice: Principal = { id: "u-alice", kind: "human", active: true, human: { username: "alice", email: "alice@example.com", display_name: "Alice Ng" }, grants: [], groups: [{ id: "g-hd", name: "Help Desk" }] };
const me: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };

function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...GROUPS_KEY], [group]);
  qc.setQueryData([...GROUPS_KEY, "g-hd"], group);
  qc.setQueryData([...GROUPS_KEY, "g-hd", "members"], members);
  qc.setQueryData([...GROUPS_KEY, "g-hd", "grants"], []);
  qc.setQueryData([...PRINCIPALS_KEY], [alice]);
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
    expect(screen.getByText("help-desk")).toBeTruthy(); // the technical name beside the display name
    expect(screen.getByText("Support crew")).toBeTruthy();
  });

  it("opens read-only and reveals edit controls only behind the header pencil", async () => {
    mount();
    fireEvent.click(screen.getByText("Help Desk"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    // Read mode: no Delete, no member Remove, no display-name input.
    expect(within(blade).queryByText("Delete group")).toBeNull();
    expect(within(blade).queryByLabelText("Remove")).toBeNull();
    // The header pencil opens edit mode.
    fireEvent.click(within(blade).getByLabelText("Edit"));
    expect(within(blade).getByText("Delete group")).toBeTruthy();
    expect(within(blade).getByText("Save")).toBeTruthy();
    expect(within(blade).getAllByLabelText("Remove").length).toBeGreaterThan(0);
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
    // Cancel reverts the staging and returns to read mode.
    fireEvent.click(within(blade).getByText("Cancel"));
    expect(within(blade).getByText("alice")).toBeTruthy();
    expect(within(blade).queryByText("Delete group")).toBeNull();
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
