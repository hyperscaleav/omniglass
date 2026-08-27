import { describe, it, expect, afterEach } from "vitest";
import { render, screen, cleanup } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import FleetRedirect from "./FleetRedirect";

// The old index URLs land somewhere sensible (#798): the bare /locations,
// /systems, and /components addresses redirect into the fleet's list face on
// the matching kind tab. The :id detail routes are untouched; only the index
// address moved.
afterEach(cleanup);

function mountAt(path: string) {
  window.history.pushState({}, "", path);
  return render(() => (
    <Router base="/web">
      <Route path="/locations" component={FleetRedirect} />
      <Route path="/systems" component={FleetRedirect} />
      <Route path="/components" component={FleetRedirect} />
      <Route path="/fleet" component={() => <div data-testid="fleet-page" />} />
    </Router>
  ));
}

describe("the re-homed index addresses", () => {
  it.each([
    ["/web/locations", "locations"],
    ["/web/systems", "systems"],
    ["/web/components", "components"],
  ])("%s lands on the fleet list's %s tab", async (path, kind) => {
    mountAt(path);
    expect(await screen.findByTestId("fleet-page")).toBeTruthy();
    expect(window.location.pathname).toBe("/web/fleet");
    expect(window.location.search).toContain("view=list");
    expect(window.location.search).toContain(`kind=${kind}`);
  });
});
