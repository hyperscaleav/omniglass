import { describe, it, expect } from "vitest";
import { render } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import HealthBadge from "./HealthBadge";
import { systemHealthKey, locationHealthKey, type EstateHealth } from "../lib/health";

// The badge is the one health chip: three distinct states, each carrying the WORD,
// so the verdict never depends on hue alone. It reads either a verdict the caller
// already holds or one it fetches itself (the systems list has no bulk health read,
// so each row's badge owns its query and shares the panel's cache key).
function mount(el: () => unknown, seed?: { key: readonly unknown[]; data: EstateHealth }) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  if (seed) qc.setQueryData([...seed.key], seed.data);
  return render(() => <QueryClientProvider client={qc}>{el() as never}</QueryClientProvider>);
}

const health = (verdict: string, owner: string): EstateHealth => ({
  owner,
  owner_kind: "system",
  verdict,
  roles: [],
  systems: [],
  transitions: [],
});

describe("HealthBadge", () => {
  it("names each verdict in words, not colour alone", () => {
    for (const v of ["healthy", "degraded", "outage"]) {
      const { getByText, unmount } = mount(() => <HealthBadge verdict={v} />);
      expect(getByText(v)).toBeTruthy();
      unmount();
    }
  });

  it("gives each verdict its own semantic hue, so the three read as distinct states", () => {
    const seen = new Set<string>();
    for (const v of ["healthy", "degraded", "outage"]) {
      const { getByText, unmount } = mount(() => <HealthBadge verdict={v} />);
      const cls = getByText(v).className;
      expect(cls).toMatch(/badge-(success|warning|error)/);
      seen.add(cls.match(/badge-(success|warning|error)/)![0]);
      unmount();
    }
    expect(seen.size).toBe(3); // never one accent for "not fine"
  });

  it("reads a system's verdict from the cache the panel shares", () => {
    const { getByText } = mount(() => <HealthBadge system="boardroom" />, {
      key: systemHealthKey("boardroom"),
      data: health("outage", "boardroom"),
    });
    expect(getByText("outage")).toBeTruthy();
  });

  it("reads a location's verdict from its own namespace, so a shared name cannot collide", () => {
    const { getByText } = mount(() => <HealthBadge location="boardroom" />, {
      key: locationHealthKey("boardroom"),
      data: health("degraded", "boardroom"),
    });
    expect(getByText("degraded")).toBeTruthy();
  });

  it("says unknown rather than guessing when no verdict has been read", () => {
    const { getByText } = mount(() => <HealthBadge verdict="" />);
    expect(getByText("unknown")).toBeTruthy();
  });

  it("renders nothing at all when quiet, so a list never flashes a column of unknowns", () => {
    const { container } = mount(() => <HealthBadge verdict="" quiet />);
    expect(container.textContent).toBe("");
  });
});
