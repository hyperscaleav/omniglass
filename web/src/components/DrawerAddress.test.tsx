import "@testing-library/jest-dom/vitest";
import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import { render, fireEvent, screen, waitFor, cleanup } from "@solidjs/testing-library";
import { Router, Route, useLocation } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import FlatList from "./FlatList";
import TreeList, { type ListConfig, type ListNode } from "./TreeList";
import type { BladeDef } from "../lib/blades";

// An open drawer has an address (#2429): the list primitive writes `?open=<row
// address>` on open (a history push), clears it on close, opens the row a loaded
// address names, closes on Back, and shows a non-disclosing notice for an
// address that names nothing. Pinned on both bodies: FlatList (drawer and blade
// stack) and TreeList.
afterEach(cleanup);

type R = { id: string };
const rows: R[] = [{ id: "alpha" }, { id: "beta" }];
const filterKeys = [{ key: "name", type: "string" as const, hint: "substring", get: (r: R) => r.id, values: () => [] }];
const columns = [{ key: "id", label: "Id", cell: (r: R) => <span data-testid={`cell-${r.id}`}>{r.id}</span> }];
const registry: Record<string, BladeDef> = {
  thing: { Title: (p) => <>blade {p.id}</>, Body: (p) => <div>blade-body {p.id}</div> },
};

function Where() {
  const l = useLocation();
  return <div data-testid="where">{l.pathname + l.search}</div>;
}

function mountFlat(kind: "drawer" | "blade", at = "/list", extra: Partial<Parameters<typeof FlatList<R>>[0]["config"]> = {}) {
  window.history.pushState({}, "", at);
  return render(() => (
    <Router>
      <Route
        path="/list"
        component={() => (
          <>
            <Where />
            <FlatList<R>
              config={{
                entity: { name: "thing", plural: "things" },
                rows: () => rows,
                filterKeys,
                columns,
                rowId: (r) => r.id,
                ...(kind === "drawer"
                  ? { detail: (r: R) => ({ title: <span>drawer {r.id}</span>, body: <div>drawer-body {r.id}</div> }) }
                  : { blades: { registry, rootKind: "thing" } }),
                ...extra,
              }}
            />
          </>
        )}
      />
    </Router>
  ));
}

const where = () => screen.getByTestId("where").textContent;

for (const kind of ["drawer", "blade"] as const) {
  describe(`FlatList drawer address (${kind})`, () => {
    it("writes ?open= on open and keeps the page's other params", async () => {
      mountFlat(kind, "/list?sort=id");
      fireEvent.click(screen.getByTestId("cell-beta"));
      expect(await screen.findByText(`${kind}-body beta`)).toBeInTheDocument();
      expect(where()).toBe("/list?sort=id&open=beta");
    });

    it("opens the row a loaded address names", async () => {
      mountFlat(kind, "/list?open=alpha&sort=id");
      expect(await screen.findByText(`${kind}-body alpha`)).toBeInTheDocument();
      expect(where()).toBe("/list?open=alpha&sort=id");
    });

    it("clears the address when the drawer closes, leaving the other params", async () => {
      mountFlat(kind, "/list?open=alpha&sort=id");
      await screen.findByText(`${kind}-body alpha`);
      fireEvent.click(screen.getByRole("button", { name: /close/i }));
      await waitFor(() => expect(screen.queryByText(`${kind}-body alpha`)).toBeNull());
      await waitFor(() => expect(where()).toBe("/list?sort=id"));
    });

    it("Back closes the drawer without leaving the list", async () => {
      mountFlat(kind);
      fireEvent.click(screen.getByTestId("cell-alpha"));
      await screen.findByText(`${kind}-body alpha`);
      window.history.back();
      await waitFor(() => expect(screen.queryByText(`${kind}-body alpha`)).toBeNull());
      expect(where()).toBe("/list");
      expect(screen.getByTestId("cell-alpha")).toBeInTheDocument();
    });

    it("shows a not-found notice and no drawer for an unknown address, with no console error", async () => {
      const err = vi.spyOn(console, "error").mockImplementation(() => {});
      mountFlat(kind, "/list?open=nope");
      const notice = await screen.findByTestId("drawer-miss");
      expect(notice).toHaveTextContent("nope");
      expect(screen.queryByText(/-body/)).toBeNull();
      expect(screen.getByTestId("cell-alpha")).toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
      await waitFor(() => expect(screen.queryByTestId("drawer-miss")).toBeNull());
      expect(where()).toBe("/list");
      expect(err).not.toHaveBeenCalled();
      err.mockRestore();
    });

    it("does not flash the notice while the list is still loading", async () => {
      mountFlat(kind, "/list?open=alpha", { loading: () => true, rows: () => [] });
      expect(screen.queryByTestId("drawer-miss")).toBeNull();
    });

    it("honors a legacy id param and sweeps it on the next write", async () => {
      mountFlat(kind, "/list?g=alpha", { legacyParam: "g" });
      expect(await screen.findByText(`${kind}-body alpha`)).toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: /close/i }));
      await waitFor(() => expect(screen.queryByText(`${kind}-body alpha`)).toBeNull());
      await waitFor(() => expect(where()).toBe("/list"));
    });
  });
}

describe("FlatList blade stack addressing", () => {
  it("keeps a nested blade out of the URL", async () => {
    const nested: Record<string, BladeDef> = {
      thing: { Title: (p) => <>blade {p.id}</>, Body: (p) => <div>blade-body {p.id}</div> },
    };
    mountFlat("blade", "/list?open=alpha", { blades: { registry: nested, rootKind: "thing" } });
    await screen.findByText("blade-body alpha");
    expect(where()).toBe("/list?open=alpha");
  });
});

// TreeList (the Files page's body): same contract over its node ids.
type N = ListNode & { label: string };
function treeCfg(): ListConfig<N> {
  const nodes: N[] = [
    { id: "f1", display: "first", children: [], label: "first" },
    { id: "f2", display: "second", children: [], label: "second" },
  ];
  return {
    entity: { name: "file", plural: "Files" },
    storageKey: "og-test-drawer",
    flat: true,
    nodes: () => nodes,
    columns: {},
    columnKeys: [],
    defaultCols: [],
    filterKeys: [{ key: "name", type: "string", hint: "substring", get: (n: N) => n.display, values: () => [] }],
    renderDetail: (n: N) => <div>tree-body {n.display}</div>,
  } as unknown as ListConfig<N>;
}

function mountTree(at: string) {
  window.history.pushState({}, "", at);
  const qc = new QueryClient();
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route
          path="/tree"
          component={() => (
            <>
              <Where />
              <TreeList<N> config={treeCfg()} />
            </>
          )}
        />
      </Router>
    </QueryClientProvider>
  ));
}

describe("TreeList drawer address", () => {
  beforeEach(() => localStorage.clear());

  it("writes ?open= on open, opens a loaded address, and Back closes it", async () => {
    mountTree("/tree");
    fireEvent.click(screen.getByText("second"));
    expect(await screen.findByText("tree-body second")).toBeInTheDocument();
    expect(where()).toBe("/tree?open=f2");
    window.history.back();
    await waitFor(() => expect(screen.queryByText("tree-body second")).toBeNull());
    expect(where()).toBe("/tree");
  });

  it("opens the node a loaded address names and clears it on close", async () => {
    mountTree("/tree?open=f1");
    expect(await screen.findByText("tree-body first")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /close/i }));
    await waitFor(() => expect(screen.queryByText("tree-body first")).toBeNull());
    await waitFor(() => expect(where()).toBe("/tree"));
  });

  it("shows the not-found notice for an unknown address", async () => {
    mountTree("/tree?open=zzz");
    expect(await screen.findByTestId("drawer-miss")).toHaveTextContent("zzz");
  });
});
