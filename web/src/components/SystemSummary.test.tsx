import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import SystemSummary from "./SystemSummary";
import { FLEET_VIEW_KEY, type FleetView } from "../lib/fleet";
import { systemHealthKey, type FleetHealth } from "../lib/health";
import { SYSTEMS_KEY } from "../lib/systems";
import { STANDARDS_KEY } from "../lib/standards";
import { uuidFor } from "../lib/testids";
import { MAJOR_LEVEL, SEEDED_LEVELS, seedSeverityLevels } from "../lib/testseverity";

// The brief system card a place holding several systems shows (#872). Its alarm
// lines wear the severity registry's label and color (#921), worst first, so an
// operator level renders exactly like a shipped one with no console change.

const sys = uuidFor("ss-sys");
const view = {
  locations: [],
  systems: [{ id: sys, name: "huddle", label: "Huddle", location: null, verdict: "degraded", dots: [] }],
} as unknown as FleetView;
const health = {
  verdict: "degraded",
  roles: [
    {
      name: "display", label: "Display", impact: "degraded", quorum: 1, satisfying: 1, short: 0, spare: 0,
      impaired: true, active: true, assigned_to: ["display-1"], down: [],
      alarms: [
        { id: "al-1", component: "display-1", component_id: uuidFor("ss-c1"), severity: "major", message: "Lamp hours high", raised_at: "2026-10-18T10:00:00Z" },
        { id: "al-2", component: "display-1", component_id: uuidFor("ss-c1"), severity: "warning", message: "Fan slow", raised_at: "2026-10-18T11:00:00Z" },
      ],
    },
  ],
  systems: [],
  transitions: [],
} as unknown as FleetHealth;

function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...FLEET_VIEW_KEY], view);
  qc.setQueryData([...systemHealthKey(sys)], health);
  qc.setQueryData([...SYSTEMS_KEY], []);
  qc.setQueryData([...STANDARDS_KEY], []);
  seedSeverityLevels(qc, [...SEEDED_LEVELS, MAJOR_LEVEL]);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router><Route path="*" component={() => <SystemSummary systemId={sys} />} /></Router>
    </QueryClientProvider>
  ));
}

describe("SystemSummary", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders each alarm's severity by the registry's label and color, worst first", () => {
    mount();
    const card = screen.getByTestId(`system-summary-${sys}`);
    const chips = [...card.querySelectorAll<HTMLElement>("[data-severity]")];
    expect(chips.map((c) => c.textContent)).toEqual(["Major", "Warning"]);
    expect(chips[0].classList.contains("badge-average")).toBe(true);
    expect(chips[1].className).toContain("badge-warning");
    expect(within(card).getByText("Lamp hours high")).toBeTruthy();
  });
});
