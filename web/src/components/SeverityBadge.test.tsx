import { describe, it, expect, vi, afterEach } from "vitest";
import { render } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import SeverityBadge from "./SeverityBadge";
import { MAJOR_LEVEL, SEEDED_LEVELS, seedSeverityLevels } from "../lib/testseverity";

// The severity chip renders the registry's label in the registry's color (#917),
// and a level name it cannot resolve as itself in the neutral chip.
function mount(name: string, levels = SEEDED_LEVELS, seed = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  if (seed) seedSeverityLevels(qc, levels);
  return render(() => (
    <QueryClientProvider client={qc}>
      <SeverityBadge name={name} />
    </QueryClientProvider>
  ));
}

describe("SeverityBadge", () => {
  afterEach(() => vi.restoreAllMocks());

  it.each([
    ["info", "Info", "badge-ghost"],
    ["warning", "Warning", "badge-warning"],
    ["average", "Average", "badge-average"],
    ["high", "High", "badge-error"],
    ["disaster", "Disaster", "badge-disaster"],
  ])("renders %s with its label and class", (name, label, cls) => {
    const { getByText } = mount(name);
    const el = getByText(label);
    expect(el.classList.contains(cls)).toBe(true);
    expect(el.dataset.severity).toBe(name);
  });

  it("says in its hint what the level does to its component", () => {
    expect(mount("high").getByText("High").title).toMatch(/puts its component in outage/);
    expect(mount("average").getByText("Average").title).toMatch(/degrades its component/);
  });

  it("renders an operator-defined level from the registry alone", () => {
    const el = mount("major", [...SEEDED_LEVELS, MAJOR_LEVEL]).getByText("Major");
    expect(el.classList.contains("badge-average")).toBe(true);
  });

  it("falls back to the raw name in the neutral chip while the levels load", () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(() => new Promise(() => {}));
    const el = mount("high", SEEDED_LEVELS, false).getByText("high");
    expect(el.classList.contains("badge-ghost")).toBe(true);
  });
});
