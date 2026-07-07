import { describe, it, expect, vi } from "vitest";
import { createSignal } from "solid-js";
import { render, fireEvent } from "@solidjs/testing-library";
import GrantBuilder from "./GrantBuilder";
import type { TreeNode } from "../lib/treeselect";
import type { ExistingGrant } from "../lib/grantdraft";

// A component test of the staged grant builder: it drives the real role -> kind ->
// entity keyboard pipeline and the mark-for-removal chips, and asserts the diff
// handed to onSave. The staging semantics are unit-tested in lib/grantdraft; this
// proves the wiring, including that nothing is applied until Save (stage -> preview
// -> save).
const roles = ["admin", "viewer", "operator"];
const locNodes: TreeNode[] = [
  { id: "loc-boi", value: "loc-boi", label: "boi", parentId: null, rank: 0 },
  { id: "loc-sjc", value: "loc-sjc", label: "sjc", parentId: null, rank: 0 },
];
const entities = (kind: "location" | "system" | "component"): TreeNode[] => (kind === "location" ? locNodes : []);
const scopeName = (id: string): string | undefined => ({ "loc-boi": "boi", "loc-sjc": "sjc" })[id];

function mount(current: ExistingGrant[] = []) {
  const onSave = vi.fn().mockResolvedValue(undefined);
  const utils = render(() => (
    <GrantBuilder principalId="p1" current={current} roles={roles} entities={entities} scopeName={scopeName} canGrant canRevoke onSave={onSave} />
  ));
  return { onSave, ...utils };
}

const typeEnter = (input: HTMLElement, value: string) => {
  fireEvent.input(input, { target: { value } });
  fireEvent.keyDown(input, { key: "Enter" });
};

describe("GrantBuilder", () => {
  it("stages an all-scope grant through the keyboard pipeline and applies it only on save", () => {
    const { onSave, getByRole, queryByLabelText, getByText } = mount([]);
    const input = getByRole("combobox");
    typeEnter(input, "admin"); // pick the role
    typeEnter(input, "all"); // pick the scope kind -> commits the chip

    // The chip is staged (added), and nothing has been sent yet.
    expect(queryByLabelText("Remove staged admin @ all")).toBeTruthy();
    expect(onSave).not.toHaveBeenCalled();

    fireEvent.click(getByText("Save grants"));
    expect(onSave).toHaveBeenCalledTimes(1);
    expect(onSave.mock.calls[0][0]).toEqual({ adds: [{ role: "admin", scope_kind: "all", scope_id: undefined }], removes: [] });
  });

  it("stages a scoped grant (role -> kind -> entity), resolving the entity name in the chip", () => {
    const { getByRole, queryByLabelText } = mount([]);
    const input = getByRole("combobox");
    typeEnter(input, "viewer"); // role
    typeEnter(input, "location"); // scope kind
    typeEnter(input, "boi"); // entity by name

    expect(queryByLabelText("Remove staged viewer @ location:boi")).toBeTruthy();
  });

  it("marks an existing grant for removal and saves it as a revoke, not a live delete", () => {
    const current: ExistingGrant[] = [{ id: "g1", role: "viewer", scope_kind: "all" }];
    const { onSave, getByLabelText, getByText } = mount(current);

    // Removing marks the chip; nothing is sent.
    fireEvent.click(getByLabelText("Remove viewer @ all"));
    expect(onSave).not.toHaveBeenCalled();

    fireEvent.click(getByText("Save grants"));
    expect(onSave.mock.calls[0][0]).toEqual({ adds: [], removes: [{ id: "g1", role: "viewer", scope_kind: "all" }] });
  });

  it("undoes a marked removal, leaving nothing to save", () => {
    const current: ExistingGrant[] = [{ id: "g1", role: "viewer", scope_kind: "all" }];
    const { getByLabelText, queryByText } = mount(current);
    fireEvent.click(getByLabelText("Remove viewer @ all"));
    expect(queryByText("Save grants")).toBeTruthy(); // dirty
    fireEvent.click(getByLabelText("Restore viewer @ all"));
    expect(queryByText("Save grants")).toBeFalsy(); // clean again
  });

  it("rejects staging a grant that is already held", () => {
    const current: ExistingGrant[] = [{ id: "g1", role: "viewer", scope_kind: "all" }];
    const { getByRole, queryByText } = mount(current);
    const input = getByRole("combobox");
    typeEnter(input, "viewer");
    typeEnter(input, "all");
    expect(queryByText(/already granted/i)).toBeTruthy();
  });

  // The entity suggestions read the tree through props.entities(), which closes
  // over a query signal. Solid tracks that read transitively, so entity data that
  // arrives after mount (a refetch) is reflected in the dropdown without any
  // explicit version prop.
  it("re-tracks entity data that arrives after the entity stage is reached", () => {
    const [locs, setLocs] = createSignal<TreeNode[]>([]);
    const onSave = vi.fn().mockResolvedValue(undefined);
    const { getByRole, queryByLabelText } = render(() => (
      <GrantBuilder
        principalId="p1"
        current={[]}
        roles={roles}
        entities={(k) => (k === "location" ? locs() : [])}
        scopeName={scopeName}
        canGrant
        canRevoke
        onSave={onSave}
      />
    ));
    const input = getByRole("combobox");
    typeEnter(input, "viewer"); // role
    typeEnter(input, "location"); // kind -> entity stage, tree still empty
    setLocs(locNodes); // the refetch lands
    typeEnter(input, "boi"); // only resolvable if the memo re-tracked the new data
    expect(queryByLabelText("Remove staged viewer @ location:boi")).toBeTruthy();
  });

  it("cancels all staged changes", () => {
    const { getByRole, queryByText } = mount([]);
    const input = getByRole("combobox");
    typeEnter(input, "admin");
    typeEnter(input, "all");
    expect(queryByText("Save grants")).toBeTruthy();
    fireEvent.click(getByText_("Cancel", queryByText));
    expect(queryByText("Save grants")).toBeFalsy();
  });
});

// getByText_ is a tiny helper so the cancel test can click the Cancel button via
// the same queryByText the assertions use (avoids a second destructured getter).
function getByText_(label: string, queryByText: (t: string) => HTMLElement | null): HTMLElement {
  const el = queryByText(label);
  if (!el) throw new Error(`no element with text ${label}`);
  return el;
}
