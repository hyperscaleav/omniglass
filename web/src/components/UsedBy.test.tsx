import "@testing-library/jest-dom/vitest";
import { describe, expect, it } from "vitest";
import { fireEvent, render, screen, within } from "@solidjs/testing-library";
import UsedBy, { type UsedByGroup } from "./UsedBy";
import EntityLink from "./EntityLink";

const rows = (n: number) =>
  Array.from({ length: n }, (_, i) => ({ key: `p${i}`, element: <EntityLink kind="product" address={`prod-${i}`} /> }));

const group = (over: Partial<UsedByGroup> = {}): UsedByGroup => ({ label: "Products", count: 2, rows: rows(2), ...over });

describe("UsedBy", () => {
  it("titles each group with its count and lists its rows as links", () => {
    render(() => <UsedBy groups={[group(), group({ label: "Components", count: 0, rows: [] })]} />);
    const products = screen.getByTestId("used-by-products");
    expect(within(products).getByTestId("used-by-count")).toHaveTextContent("2");
    expect(within(products).getAllByRole("link").map((a) => a.getAttribute("href"))).toEqual(["/products?open=prod-0", "/products?open=prod-1"]);
    const empty = screen.getByTestId("used-by-components");
    expect(within(empty).getByTestId("used-by-count")).toHaveTextContent("0");
    expect(within(empty).getByText("None")).toBeInTheDocument();
  });

  it("shows the first few of a long list and folds the rest behind Show all", () => {
    render(() => <UsedBy groups={[group({ count: 8, rows: rows(8) })]} />);
    const g = screen.getByTestId("used-by-products");
    expect(within(g).getAllByRole("link")).toHaveLength(5);
    fireEvent.click(within(g).getByRole("button", { name: "Show all 8" }));
    expect(within(g).getAllByRole("link")).toHaveLength(8);
    expect(within(g).queryByRole("button", { name: /show all/i })).toBeNull();
  });

  it("offers See all when the server capped the page below the true count", () => {
    render(() => <UsedBy groups={[group({ label: "Components", count: 41, rows: rows(5), seeAll: { href: "/explore?chips=x" } })]} />);
    const g = screen.getByTestId("used-by-components");
    expect(within(g).getByTestId("used-by-count")).toHaveTextContent("41");
    expect(within(g).getByRole("link", { name: "See all 41" })).toHaveAttribute("href", "/explore?chips=x");
  });

  it("offers no See all when every row is in hand", () => {
    render(() => <UsedBy groups={[group({ seeAll: { href: "/explore" } })]} />);
    expect(screen.queryByRole("link", { name: /see all/i })).toBeNull();
  });

  it("says the read is loading rather than reporting zero", () => {
    render(() => <UsedBy groups={[group({ loading: true, count: 0, rows: [] })]} />);
    const g = screen.getByTestId("used-by-products");
    expect(within(g).getByTestId("used-by-count")).toHaveTextContent("…");
    expect(within(g).queryByText("None")).toBeNull();
  });
});
