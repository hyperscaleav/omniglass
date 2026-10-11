import { describe, it, expect, afterEach } from "vitest";
import { render, screen, cleanup } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import FleetRedirect from "./FleetRedirect";

// The old index addresses land on the outline (#798, #826, #861): the bare
// /locations, /systems and /components URLs, the retired /fleet canvas, and
// the list address main once sent every index visit to. Every place,
// system and component is in the one outline now, so they all land there.
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

describe("the re-homed addresses", () => {
  it.each(["/web/locations", "/web/systems", "/web/components", "/web/fleet", "/web/fleet?view=list&kind=systems"])("%s lands on the outline", async (path) => {
    mountAt(path);
    expect(await screen.findByTestId("explore-page")).toBeTruthy();
    expect(window.location.pathname).toBe("/web/explore");
    expect(window.location.search).toBe("");
  });
});
