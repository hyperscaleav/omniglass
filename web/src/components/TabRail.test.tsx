import { describe, it, expect, afterEach } from "vitest";
import { render, screen, cleanup, fireEvent, waitFor, within } from "@solidjs/testing-library";
import { Router, Route, useSearchParams } from "@solidjs/router";
import TabRail from "./TabRail";

// The workspace tab rail (#790): a facet of one page is a URL fact (`?tab=`),
// so every view is an address (#763's rule). One tab is no workspace: the
// rail renders only once a second facet exists.
afterEach(cleanup);

function mount(tabs: { key: string; label: string }[], path = "/x") {
  window.history.pushState({}, "", path);
  return render(() => (
    <Router>
      <Route
        path="/x"
        component={() => {
          const [params] = useSearchParams();
          return (
            <div>
              <TabRail tabs={tabs} />
              <div data-testid="active">{(Array.isArray(params.tab) ? params.tab[0] : params.tab) ?? "(none)"}</div>
            </div>
          );
        }}
      />
    </Router>
  ));
}

const TABS = [
  { key: "overview", label: "Overview" },
  { key: "history", label: "History" },
];

describe("TabRail", () => {
  it("renders one tab per facet, the first active by default, and never a rail for a single facet", () => {
    mount(TABS);
    const rail = screen.getByTestId("tab-rail");
    expect(within(rail).getByRole("tab", { name: "Overview" }).getAttribute("aria-selected")).toBe("true");
    cleanup();
    mount([{ key: "overview", label: "Overview" }]);
    expect(screen.queryByTestId("tab-rail")).toBeNull();
  });

  it("selecting a tab writes the param; the default tab strips it back off", async () => {
    mount(TABS);
    fireEvent.click(screen.getByRole("tab", { name: "History" }));
    await waitFor(() => expect(screen.getByTestId("active").textContent).toBe("history"));
    fireEvent.click(screen.getByRole("tab", { name: "Overview" }));
    await waitFor(() => expect(screen.getByTestId("active").textContent).toBe("(none)"));
  });

  it("a deep link lands on its tab", () => {
    mount(TABS, "/x?tab=history");
    expect(screen.getByRole("tab", { name: "History" }).getAttribute("aria-selected")).toBe("true");
  });
});
