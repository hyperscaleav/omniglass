import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { confirmAction, confirmDelete } from "../lib/confirm";
import { uuidFor } from "../lib/testids";

const EP = uuidFor("icmp");
const CARD = uuidFor("codec-card");

// The console's one destructive confirm (#2463): confirmDelete and
// confirmAction open the one ConfirmDialog, which names the thing, says what
// goes with it, and carries the verb; a request with run acts in the dialog,
// and a refused delete that names its blockers lists them with their own
// links and deletes, so the operator clears the way and retries in place.

const deleteEndpoint = vi.fn();
const deleteComponent = vi.fn();
vi.mock("../lib/endpoints", () => ({ deleteEndpoint: (id: string) => deleteEndpoint(id) }));
vi.mock("../lib/components", () => ({ deleteComponent: (id: string) => deleteComponent(id) }));

beforeEach(() => {
  deleteEndpoint.mockReset();
  deleteComponent.mockReset();
});

const dialog = () => screen.getByRole("alertdialog");

describe("confirmDelete", () => {
  it("names the thing and the consequence, and the destructive button carries the verb", async () => {
    const answer = confirmDelete({ kind: "tag", name: "floor-3", consequence: "Its bindings across the fleet are removed too." });
    const d = await screen.findByRole("alertdialog", { name: 'Delete tag "floor-3"?' });
    expect(d.textContent).toContain("Its bindings across the fleet are removed too.");
    const go = within(d).getByRole("button", { name: "Delete tag" });
    expect(go.className).toContain("btn-danger");
    fireEvent.click(go);
    await expect(answer).resolves.toBe(true);
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  });

  it("says it cannot be undone when the site names no consequence, and takes a verb", async () => {
    void confirmDelete({ kind: "simulated component", name: "bar-1", verb: "Remove" });
    const d = await screen.findByRole("alertdialog", { name: 'Remove simulated component "bar-1"?' });
    expect(d.textContent).toContain("This cannot be undone.");
    expect(within(d).getByRole("button", { name: "Remove simulated component" })).toBeTruthy();
  });

  it("resolves false on Cancel and on Escape, and never runs the act", async () => {
    const run = vi.fn();
    const first = confirmDelete({ kind: "tag", name: "a", run });
    fireEvent.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Cancel" }));
    await expect(first).resolves.toBe(false);
    const second = confirmAction({ title: "Revoke all your sessions?", confirmLabel: "Revoke sessions" });
    fireEvent.keyDown(await screen.findByRole("alertdialog"), { key: "Escape" });
    await expect(second).resolves.toBe(false);
    expect(run).not.toHaveBeenCalled();
  });

  it("keeps the dialog open on a failed act and says why", async () => {
    const answer = confirmDelete({ kind: "tag", name: "a", run: () => Promise.reject({ status: 500, detail: "the database is away" }) });
    fireEvent.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Delete tag" }));
    await waitFor(() => expect(within(dialog()).getByRole("alert").textContent).toContain("the database is away"));
    expect(within(dialog()).queryByTestId("delete-blockers")).toBeNull();
    fireEvent.click(within(dialog()).getByRole("button", { name: "Cancel" }));
    await expect(answer).resolves.toBe(false);
  });
});

describe("a refused delete in the dialog", () => {
  const refusal = {
    status: 409,
    detail: "component still owns 1 endpoint: delete or reassign its endpoints first",
    blockers: [
      { kind: "endpoint", id: EP, name: "icmp", label: "icmp" },
      { kind: "component", id: CARD, name: "codec-card", label: "Codec card" },
    ],
    blockers_hidden: 1,
  };

  it("lists the blockers with links, counts what it cannot name, and retries once they are cleared", async () => {
    let calls = 0;
    const run = vi.fn(() => (++calls === 1 ? Promise.reject(refusal) : Promise.resolve()));
    deleteEndpoint.mockResolvedValue(undefined);
    deleteComponent.mockResolvedValue(undefined);
    const answer = confirmDelete({ kind: "component", name: "codec", run });
    fireEvent.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Delete component" }));

    const list = await within(dialog()).findByTestId("delete-blockers");
    // The server's sentence is the list's lede, not an alert beside it.
    expect(within(list).getByRole("status").textContent).toContain("still owns 1 endpoint");
    expect(within(dialog()).queryByRole("alert")).toBeNull();
    const card = within(list).getByRole("link", { name: "Codec card" });
    expect(card.getAttribute("href")).toBe(`/components/${CARD}`);
    // An endpoint has no page of its own: named, not linked.
    expect(within(list).queryByRole("link", { name: "icmp" })).toBeNull();
    expect(list.textContent).toContain("1 more you cannot see");

    // Two steps: Delete arms, the second click deletes by the blocker's own kind.
    fireEvent.click(within(list).getByRole("button", { name: "Delete endpoint icmp" }));
    expect(deleteEndpoint).not.toHaveBeenCalled();
    fireEvent.click(within(list).getByRole("button", { name: "Confirm delete endpoint icmp" }));
    await waitFor(() => expect(deleteEndpoint).toHaveBeenCalledWith(EP));
    await waitFor(() => expect(list.querySelector('[data-blocker="endpoint:icmp"]')).toBeNull());

    fireEvent.click(within(list).getByRole("button", { name: "Delete component Codec card" }));
    fireEvent.click(within(list).getByRole("button", { name: "Confirm delete component Codec card" }));
    await waitFor(() => expect(deleteComponent).toHaveBeenCalledWith(CARD));
    // Cleared, the refusal's sentence goes with the last row.
    await waitFor(() => expect(list.textContent).toContain("1 more you cannot see"));

    // Retry in place: the same button runs the delete again.
    fireEvent.click(within(dialog()).getByRole("button", { name: "Delete component" }));
    await expect(answer).resolves.toBe(true);
    expect(run).toHaveBeenCalledTimes(2);
  });

  it("shows a blocker's own refusal on its row and keeps it listed", async () => {
    deleteComponent.mockRejectedValue({ status: 409, detail: "component has child components" });
    void confirmDelete({ kind: "component", name: "codec", run: () => Promise.reject(refusal) });
    fireEvent.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Delete component" }));
    const list = await within(dialog()).findByTestId("delete-blockers");
    fireEvent.click(within(list).getByRole("button", { name: "Delete component Codec card" }));
    fireEvent.click(within(list).getByRole("button", { name: "Confirm delete component Codec card" }));
    await waitFor(() => expect(list.textContent).toContain("component has child components"));
    expect(within(list).getByRole("link", { name: "Codec card" })).toBeTruthy();
  });
});
