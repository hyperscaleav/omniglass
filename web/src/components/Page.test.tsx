import { describe, it, expect } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Page from "./Page";
import TopBar from "./TopBar";
import { pageHeader } from "../lib/handover";
import { navByPath } from "../lib/nav";

// The header every page wears: a title and a one-line hint. A page that names its
// route gets both from the nav metadata, so the sidebar, the top bar and the page
// say the same words (#2447).
function wrap(inner: () => any) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={inner} />
      </Router>
    </QueryClientProvider>
  ));
}

function shell(page: () => any, section = "Admin") {
  return wrap(() => (
    <>
      <TopBar section={section} title={pageHeader()?.title} hint={pageHeader()?.hint} onOpenPalette={() => {}} />
      {page()}
    </>
  ));
}

describe("Page header from the nav metadata", () => {
  it("renders the one h1 in the top bar with the hint beside it, none in the page", () => {
    shell(() => <Page route="/roles"><p>body</p></Page>);
    const h1s = screen.getAllByRole("heading", { level: 1 });
    expect(h1s).toHaveLength(1);
    expect(h1s[0].textContent).toBe("Roles");
    expect(h1s[0].closest("header")).not.toBeNull();
    expect(screen.getByTestId("page-header").textContent).toContain(navByPath["/roles"].hint);
    expect(screen.getByTestId("page-header").closest("header")).not.toBeNull();
  });

  it("a page without a Page header gets its label as the bar's h1", () => {
    wrap(() => <TopBar section="Profile" title="Alarm subscriptions" onOpenPalette={() => {}} />);
    const h1s = screen.getAllByRole("heading", { level: 1 });
    expect(h1s).toHaveLength(1);
    expect(h1s[0].textContent).toBe("Alarm subscriptions");
  });

  it("lets an explicit title or subtitle win over the nav entry", () => {
    shell(() => <Page route="/roles" title="Custom" subtitle="Own words."><p>body</p></Page>);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Custom");
    expect(screen.getByTestId("page-header").textContent).toContain("Own words.");
  });

  it("the Roles hint says roles are built in", () => {
    expect(navByPath["/roles"].hint).toMatch(/built-in/i);
  });

  for (const path of ["/alarms", "/outposts", "/variables", "/secrets", "/users", "/roles", "/user-groups", "/audit", "/files"]) {
    it(`${path} has a nav label and a one-line hint`, () => {
      expect(navByPath[path]?.label).toBeTruthy();
      expect(navByPath[path]?.hint).toBeTruthy();
    });
  }
});
