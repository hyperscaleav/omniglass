import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import Breadcrumb from "./Breadcrumb";
import type { Crumb } from "../lib/breadcrumb";

afterEach(cleanup);

function mount(crumbs: Crumb[]) {
  render(() => (
    <Router>
      <Route path="*" component={() => <Breadcrumb crumbs={crumbs} />} />
    </Router>
  ));
  return within(screen.getByRole("navigation", { name: "Breadcrumb" }));
}

// A crumb with a destination is a real link (#875): middle-clickable, its
// address copyable and in the DOM, announced as a link inside the nav landmark.
describe("Breadcrumb", () => {
  it("renders a crumb with an href as a link to it", () => {
    const trail = mount([
      { key: "explore", label: "Explore", href: "/explore?node=abc" },
      { key: "hq", label: "Headquarters", href: "/locations/hq" },
      { key: "here", label: "Here" },
    ]);
    expect(trail.getByRole("link", { name: "Explore" }).getAttribute("href")).toBe("/explore?node=abc");
    expect(trail.getByRole("link", { name: "Headquarters" }).getAttribute("href")).toBe("/locations/hq");
    expect(trail.queryAllByRole("button")).toHaveLength(0);
  });

  it("renders the last crumb as the current page, not a link", () => {
    const trail = mount([
      { key: "explore", label: "Explore", href: "/explore" },
      { key: "here", label: "Here" },
    ]);
    expect(trail.queryByRole("link", { name: "Here" })).toBeNull();
    expect(trail.getByText("Here").getAttribute("aria-current")).toBe("page");
    expect(trail.queryAllByRole("link")).toHaveLength(1);
  });

  it("applies crumbLayout's truncation: earlier crumbs shrink, the last two hold", () => {
    const trail = mount([
      { key: "a", label: "A", href: "/a" },
      { key: "b", label: "B", href: "/b" },
      { key: "c", label: "C" },
    ]);
    const flexOf = (label: string) => (trail.getByText(label).closest("span[title]") as HTMLElement).style.flex;
    expect(flexOf("A")).toBe("0 1 auto");
    expect(flexOf("B")).toBe("0 0 auto");
    expect(flexOf("C")).toBe("0 0 auto");
  });
});
