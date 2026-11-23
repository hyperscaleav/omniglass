import { describe, it, expect, afterEach } from "vitest";
import { render, screen, cleanup, fireEvent, waitFor } from "@solidjs/testing-library";
import { Router, Route, useSearchParams } from "@solidjs/router";
import { createTabState, resolveTab, type TabDef } from "./tabstate";

// The detail body's tab state (#2423): one resolver for every kind (the legacy
// addresses, the ?edit= landing), held by the host. A page keeps it in the URL
// so a reload or a pasted link lands on the same facet; a blade keeps it in a
// signal, so switching a blade's tab never rewrites the page beneath it.

const TABS: TabDef[] = [
  { key: "overview", label: "Overview" },
  { key: "activity", label: "Activity" },
  { key: "configure", label: "Configure" },
];

describe("resolveTab", () => {
  it("lands on the asked tab, the default when none or an unknown one is asked", () => {
    expect(resolveTab(TABS, "activity")).toBe("activity");
    expect(resolveTab(TABS, undefined)).toBe("overview");
    expect(resolveTab(TABS, "nope")).toBe("overview");
  });

  it("maps a retired address onto the tab that absorbed it", () => {
    expect(resolveTab(TABS, "events", { legacy: { events: "activity" } })).toBe("activity");
  });

  it("lands an edit intent on Configure, and only where Configure is offered", () => {
    expect(resolveTab(TABS, undefined, { edit: "1" })).toBe("configure");
    expect(resolveTab(TABS.slice(0, 2), undefined, { edit: "1" })).toBe("overview");
    // Another editor's intent (a sole location's) does not move the rail.
    expect(resolveTab(TABS, undefined, { edit: "location" })).toBe("overview");
    // An explicit tab wins over the intent.
    expect(resolveTab(TABS, "activity", { edit: "1" })).toBe("activity");
  });
});

afterEach(cleanup);

function mount(host: "page" | "blade", path = "/x") {
  window.history.pushState({}, "", path);
  return render(() => (
    <Router>
      <Route
        path="/x"
        component={() => {
          const [params] = useSearchParams();
          const tabs = createTabState(host, () => TABS, { legacy: { events: "activity" } });
          return (
            <div>
              <button onClick={() => tabs.select("activity")}>go</button>
              <button onClick={() => tabs.select("overview")}>home</button>
              <div data-testid="active">{tabs.active()}</div>
              <div data-testid="param">{(Array.isArray(params.tab) ? params.tab[0] : params.tab) ?? "(none)"}</div>
            </div>
          );
        }}
      />
    </Router>
  ));
}

describe("createTabState", () => {
  it("on a page, writes ?tab= and strips it for the default; an old address still lands", async () => {
    mount("page", "/x?tab=events");
    expect(screen.getByTestId("active").textContent).toBe("activity");
    fireEvent.click(screen.getByText("home"));
    await waitFor(() => expect(screen.getByTestId("param").textContent).toBe("(none)"));
    fireEvent.click(screen.getByText("go"));
    await waitFor(() => expect(screen.getByTestId("param").textContent).toBe("activity"));
  });

  it("in a blade, holds the tab in a signal and never touches the page's address", async () => {
    mount("blade", "/x?tab=configure");
    // The page's own ?tab= is the page's, not the blade's.
    expect(screen.getByTestId("active").textContent).toBe("overview");
    fireEvent.click(screen.getByText("go"));
    expect(screen.getByTestId("active").textContent).toBe("activity");
    expect(screen.getByTestId("param").textContent).toBe("configure");
    expect(window.location.search).toBe("?tab=configure");
  });
});
