import "@testing-library/jest-dom/vitest";
import { describe, it, expect } from "vitest";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import FlatList from "./FlatList";
import type { BladeDef } from "../lib/blades";

// The blades config's per-row kind: a mixed-kind surface maps each row to its
// own registry blade via `rowKind`, while every single-kind page keeps passing
// only `rootKind` and behaves exactly as before. No shipped page maps rowKind
// today; this pin holds the contract for the next mixed-kind surface, and the
// fallback pin means adding one can never change a shipped page's blade routing.

type R = { id: string; kind: string };

const rows: R[] = [
  { id: "alpha", kind: "x" },
  { id: "beta", kind: "y" },
];

const registry: Record<string, BladeDef> = {
  x: { Title: (p) => <>x {p.id}</>, Body: (p) => <div>x-body {p.id}</div> },
  y: { Title: (p) => <>y {p.id}</>, Body: (p) => <div>y-body {p.id}</div> },
};

function mount(rowKind?: (r: R) => string) {
  render(() => (
    <FlatList<R>
      config={{
        entity: { name: "thing", plural: "things" },
        rows: () => rows,
        filterKeys: [{ key: "name", type: "string", hint: "substring", get: (r) => r.id, values: () => [] }],
        columns: [{ key: "id", label: "Id", cell: (r) => <span>{r.id}</span> }],
        rowId: (r) => r.id,
        blades: { registry, rootKind: "x", ...(rowKind ? { rowKind } : {}) },
      }}
    />
  ));
}

describe("FlatList blades rowKind", () => {
  it("opens each row's blade under its rowKind when the config maps one", async () => {
    mount((r) => r.kind);
    fireEvent.click(screen.getByText("beta"));
    expect(await screen.findByText("y-body beta")).toBeInTheDocument();
  });

  it("falls back to rootKind when rowKind is absent (the single-kind pages)", async () => {
    mount();
    fireEvent.click(screen.getByText("beta"));
    expect(await screen.findByText("x-body beta")).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByText("y-body beta")).toBeNull());
  });
});

// The list primitive renders rows in the order it is given (#757). Registry
// pages hand it the server's order, which is the SQL label rule, so a new list
// picks that order up by default; sorting is the operator's, by a column header.
describe("FlatList row order", () => {
  it("renders rows as received until the operator sorts a column", () => {
    const sent = [{ id: "zulu" }, { id: "alpha" }, { id: "mike" }];
    render(() => (
      <FlatList<{ id: string }>
        config={{
          entity: { name: "thing", plural: "things" },
          rows: () => sent,
          filterKeys: [{ key: "name", type: "string", hint: "substring", get: (r) => r.id, values: () => [] }],
          columns: [{ key: "id", label: "Id", sortVal: (r) => r.id, cell: (r) => <span>{r.id}</span> }],
          rowId: (r) => r.id,
        }}
      />
    ));
    const cells = screen.getAllByRole("row").slice(1).map((tr) => tr.textContent);
    expect(cells).toEqual(["zulu", "alpha", "mike"]);
  });
});
