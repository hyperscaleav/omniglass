import { describe, it, expect, afterEach } from "vitest";
import { render, screen, cleanup } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import FleetRedirect from "./FleetRedirect";

// The old index URLs land somewhere sensible (#798, #826): the bare
// /locations, /systems, and /components addresses redirect into Explore's
// table face on the matching kind tab. The :id detail routes are untouched;
// only the index address moved.
afterEach(cleanup);

function mountAt(path: string) {
  window.history.pushState({}, "", path);
  return render(() => (
    <Router base="/web">
      <Route path="/locations" component={FleetRedirect} />
      <Route path="/systems" component={FleetRedirect} />
      <Route path="/components" component={FleetRedirect} />
      <Route path="/fleet" component={FleetRedirect} />
      <Route path="/explore" component={() => <div data-testid="explore-page" />} />
    </Router>
  ));
}

describe("the re-homed index addresses", () => {
  it.each([
    ["/web/locations", "locations"],
    ["/web/systems", "systems"],
    ["/web/components", "components"],
  ])("%s lands on Explore's table face, %s tab", async (path, kind) => {
    mountAt(path);
    expect(await screen.findByTestId("explore-page")).toBeTruthy();
    expect(window.location.pathname).toBe("/web/explore");
    expect(window.location.search).toContain("face=table");
    expect(window.location.search).toContain(`kind=${kind}`);
  });
});

describe("the retired canvas address", () => {
  it("/fleet lands on Explore's tree, no face or kind param", async () => {
    mountAt("/web/fleet");
    expect(await screen.findByTestId("explore-page")).toBeTruthy();
    expect(window.location.pathname).toBe("/web/explore");
    expect(window.location.search).toBe("");
  });
});
