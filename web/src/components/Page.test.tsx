import { describe, it, expect } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import Page from "./Page";
import { navByPath } from "../lib/nav";

// The header every page wears: a title and a one-line hint. A page that names its
// route gets both from the nav metadata, so the sidebar, the top bar and the page
// say the same words (#2447).
describe("Page header from the nav metadata", () => {
  it("takes the title and the hint from the route's nav entry", () => {
    render(() => <Page route="/roles"><p>body</p></Page>);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Roles");
    expect(screen.getByTestId("page-header").textContent).toContain(navByPath["/roles"].hint);
  });

  it("lets an explicit title or subtitle win over the nav entry", () => {
    render(() => <Page route="/roles" title="Custom" subtitle="Own words."><p>body</p></Page>);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Custom");
    expect(screen.getByTestId("page-header").textContent).toContain("Own words.");
  });

  for (const path of ["/alarms", "/outposts", "/variables", "/secrets", "/users", "/roles", "/user-groups", "/audit", "/files"]) {
    it(`${path} has a nav label and a one-line hint`, () => {
      expect(navByPath[path]?.label).toBeTruthy();
      expect(navByPath[path]?.hint).toBeTruthy();
    });
  }
});
