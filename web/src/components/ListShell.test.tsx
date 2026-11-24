import "@testing-library/jest-dom/vitest";
import { describe, it, expect } from "vitest";
import { render, fireEvent, screen } from "@solidjs/testing-library";
import ListShell from "./ListShell";

type R = { name: string };
const rows: R[] = [{ name: "alpha" }, { name: "beta" }, { name: "alphabet" }];
const keys = [{ key: "name", type: "string" as const, hint: "substring" as const, get: (r: R) => r.name, values: () => [] }];

function mount(counted: boolean, trailing?: boolean) {
  render(() => (
    <ListShell<R> filterKeys={keys} rows={rows} counted={counted} trailing={trailing ? <button type="button">New</button> : undefined}>
      {(filtered) => <ul>{filtered().map((r) => <li>{r.name}</li>)}</ul>}
    </ListShell>
  ));
}

function filter(value: string) {
  const input = screen.getByRole("combobox") as HTMLInputElement;
  fireEvent.input(input, { target: { value: `name:${value}` } });
  fireEvent.keyDown(input, { key: "Enter" });
}

// #2431 item 8: the header says how many rows survive the filter.
describe("ListShell row count", () => {
  it("says nothing while no chip filters", () => {
    mount(true);
    expect(screen.queryByTestId("filter-count")).toBeNull();
  });

  it("reads n of m while a chip filters", () => {
    mount(true, true);
    filter("alpha");
    expect(screen.getByTestId("filter-count").textContent).toBe("2 of 3");
  });

  it("stays out when the body filters for itself (not counted)", () => {
    mount(false);
    filter("alpha");
    expect(screen.queryByTestId("filter-count")).toBeNull();
  });

  // One header height with or without a trailing button (#2431 item 2): the row
  // holds the button's height itself, so a list with no New sits level with its
  // siblings instead of 4 px short.
  it("holds one row height with or without a trailing control", () => {
    mount(true);
    expect(screen.getByTestId("filter-row").className).toContain("min-h-8");
  });
});
