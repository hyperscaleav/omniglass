import { describe, it, expect } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import { createSignal } from "solid-js";
import KeyedFor from "./KeyedFor";

type Item = { name: string; value: number };

// A row holds state the list does not (here, how many times it was built), so
// a rebuilt row is one whose build count restarts.
function mount(first: Item[]) {
  const [list, setList] = createSignal<Item[]>(first);
  let built = 0;
  render(() => (
    <ul>
      <KeyedFor each={list()} by={(it) => it.name} fallback={<li>none</li>}>
        {(it, i) => {
          const build = ++built;
          return <li data-testid={`row-${it().name}`} data-build={build}>{i()}:{it().value}</li>;
        }}
      </KeyedFor>
    </ul>
  ));
  return setList;
}

describe("KeyedFor", () => {
  it("keeps each row across fresh objects with the same keys, with their newest values", () => {
    const setList = mount([{ name: "a", value: 1 }, { name: "b", value: 2 }]);
    const a = screen.getByTestId("row-a");
    setList([{ name: "a", value: 10 }, { name: "b", value: 20 }]);
    expect(screen.getByTestId("row-a")).toBe(a);
    expect(a).toHaveTextContent("0:10");
    expect(screen.getByTestId("row-b")).toHaveTextContent("1:20");
  });

  it("moves a row with its key when the order changes, and builds only a new key's row", () => {
    const setList = mount([{ name: "a", value: 1 }, { name: "b", value: 2 }]);
    const a = screen.getByTestId("row-a");
    setList([{ name: "c", value: 3 }, { name: "b", value: 2 }, { name: "a", value: 1 }]);
    expect(screen.getByTestId("row-a")).toBe(a);
    expect(a).toHaveTextContent("2:1");
    expect(Array.from(document.querySelectorAll("li")).map((li) => li.dataset.testid)).toEqual(["row-c", "row-b", "row-a"]);
    expect(screen.getByTestId("row-c")).toHaveAttribute("data-build", "3");
  });

  it("drops a key's row when it leaves, and shows the fallback for no items", () => {
    const setList = mount([{ name: "a", value: 1 }]);
    setList([]);
    expect(screen.queryByTestId("row-a")).toBeNull();
    expect(screen.getByText("none")).toBeInTheDocument();
  });
});
