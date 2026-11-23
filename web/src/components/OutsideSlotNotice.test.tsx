import { describe, it, expect } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import OutsideSlotNotice from "./OutsideSlotNotice";
import type { HealthMemberBody } from "../api/api.gen";

const m = (name: string, outside: boolean, alarms = 0): HealthMemberBody =>
  ({
    name, id: `id-${name}`, health: alarms ? "outage" : "healthy", outside_slot: outside,
    alarms: Array.from({ length: alarms }, (_, i) => ({ id: `${name}-a${i}`, component: name, component_id: `id-${name}`, severity: "high", message: "off", raised_at: "2026-11-23T14:00:00Z", acknowledged: false })),
  }) as HealthMemberBody;

function mount(members: HealthMemberBody[], placeHref?: string) {
  window.history.pushState({}, "", "/systems/s1");
  return render(() => (
    <Router>
      <Route path="/systems/:id" component={() => <OutsideSlotNotice members={members} placeHref={placeHref} />} />
    </Router>
  ));
}

describe("OutsideSlotNotice", () => {
  it("names each member outside a slot, teaches that only a slot counts toward health, and offers the placement", () => {
    mount([m("amp-1", true, 1), m("disp-1", false), m("rack-1", true)], "/systems/s1?tab=configure");
    const n = screen.getByTestId("outside-slot-notice");
    expect(n.textContent).toContain("2 components are not in a slot: amp-1, rack-1.");
    expect(n.textContent).toContain("Their alarms show here, but they do not count toward this system's health until placed in a slot");
    expect(n.textContent).toContain("only from rules someone declared");
    expect(n.textContent).toContain("1 of them has an open alarm now.");
    expect(n.textContent).not.toContain("disp-1");
    expect(screen.getByTestId("place-in-slot").getAttribute("href")).toBe("/systems/s1?tab=configure");
  });

  it("offers no placement to a caller who may not configure the system", () => {
    mount([m("amp-1", true)]);
    expect(screen.queryByTestId("place-in-slot")).toBeNull();
  });

  it("says nothing when every member fills a slot", () => {
    mount([m("disp-1", false)]);
    expect(screen.queryByTestId("outside-slot-notice")).toBeNull();
  });
});
