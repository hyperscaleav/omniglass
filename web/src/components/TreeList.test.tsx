import { describe, it, expect, afterEach, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import TreeList, { type ListConfig, type ListNode } from "./TreeList";
import { ME_KEY, type Me } from "../lib/auth";

// Opening the same row twice lands on the same ?open= address and renders one drawer, so a
// double open is invisible in the DOM. Record every open the list issues to count them.
const opens: string[] = [];
vi.mock("../lib/drawer_address", async (orig) => {
  const mod = await orig<typeof import("../lib/drawer_address")>();
  return {
    ...mod,
    useDrawerAddress: (...args: Parameters<typeof mod.useDrawerAddress>) => {
      const a = mod.useDrawerAddress(...args);
      const open = a.open;
      a.open = (id: string) => { opens.push(id); open(id); };
      return a;
    },
  };
});

// The row-with-actions pattern (#974): a row is not a button. axe's
// nested-interactive rule fails a role="button" element that holds focusable
// descendants, and a row holds the expand toggle and the action buttons. The
// name cell carries the one primary control that opens the node.
type N = ListNode & { kind: string };
const node = (id: string, children: N[] = []): N => ({ id, display: id, kind: "t", children });
const admin: Me = { user: { id: "u-root", kind: "person" }, person: { username: "root" }, permissions: [">"], grants: [] };

function mount(flat: boolean, onOpenNode = vi.fn()) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], admin);
  const nodes = [node("alpha", [node("beta")]), node("gamma")];
  const config: ListConfig<N> = {
    entity: { name: "file", plural: "Files" },
    storageKey: "og-treelist-test",
    flat,
    nodes: () => nodes,
    columns: { kind: { label: "Kind", width: 100 } },
    columnKeys: ["kind"],
    defaultCols: ["kind"],
    cellFor: (_k, n) => n.kind,
    filterKeys: [],
    sortVal: (n) => n.display,
    onOpenNode,
  };
  render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={() => <TreeList config={config} />} />
      </Router>
    </QueryClientProvider>
  ));
  return onOpenNode;
}

const FOCUSABLE = "button, a[href], input, select, textarea, [tabindex]:not([tabindex='-1'])";

describe("TreeList rows", () => {
  afterEach(() => {
    cleanup();
    localStorage.clear();
  });

  for (const flat of [true, false]) {
    it(`no role=button element holds a focusable descendant (${flat ? "flat" : "tree"})`, () => {
      mount(flat);
      const buttons = document.querySelectorAll("[role='button']");
      for (const b of buttons) expect(b.querySelectorAll(FOCUSABLE).length).toBe(0);
      // And the row itself is not one.
      for (const tr of document.querySelectorAll("tbody tr")) expect(tr.getAttribute("role")).toBeNull();
    });
  }

  it("the name cell's primary control opens the node by keyboard activation and click", async () => {
    mount(true);
    const open = screen.getByRole("button", { name: /gamma/ });
    expect(open.tagName).toBe("BUTTON");
    // A native button turns Enter/Space into a click; assert the click path.
    fireEvent.click(open);
    // The open row is the URL's ?open= address now, so the drawer follows the router.
    await waitFor(() => expect(document.querySelector("aside[data-blade]")).toBeTruthy());
  });

  it("clicking elsewhere on the row still opens the node, once", async () => {
    mount(true);
    opens.length = 0;
    fireEvent.click(screen.getByText("gamma").closest("tr")!.querySelectorAll("td")[1]);
    await waitFor(() => expect(document.querySelectorAll("aside[data-blade]").length).toBe(1));
    expect(opens).toEqual(["gamma"]);
  });

  it("clicking the name button opens the node exactly once (the click does not also reach the row)", async () => {
    mount(true);
    opens.length = 0;
    fireEvent.click(screen.getByRole("button", { name: /gamma/ }));
    await waitFor(() => expect(document.querySelectorAll("aside[data-blade]").length).toBe(1));
    expect(opens).toEqual(["gamma"]);
  });
});
