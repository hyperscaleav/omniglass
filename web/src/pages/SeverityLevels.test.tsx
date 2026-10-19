import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import SeverityLevels from "./SeverityLevels";
import { ME_KEY, type Me } from "../lib/auth";
import { MAJOR_LEVEL, SEEDED_LEVELS, seedSeverityLevels } from "../lib/testseverity";

// The severity levels page (#921): the registry an event rule raises at, read
// beside the rules. Each level shows its rank, its label in its own color (the
// same chip every alarm wears), whether it takes a component out, and its
// origin; worst first, the order an operator triages in. A read view: levels are
// authored from the CLI and API today.

const owner: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };

function mount(levels = [...SEEDED_LEVELS, MAJOR_LEVEL]) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], owner);
  seedSeverityLevels(qc, levels);
  return render(() => (
    <QueryClientProvider client={qc}>
      <SeverityLevels />
    </QueryClientProvider>
  ));
}

const bodyRows = () => screen.getAllByRole("row").filter((r) => r.querySelector("td"));

describe("Severity levels page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists every level worst first, an operator level in its ranked place", () => {
    mount();
    const names = bodyRows().map((r) => r.querySelector("td")!.textContent);
    expect(names.map((n) => n?.replace(/\s+/g, " "))).toEqual([
      expect.stringContaining("disaster"),
      expect.stringContaining("high"),
      expect.stringContaining("major"),
      expect.stringContaining("average"),
      expect.stringContaining("warning"),
      expect.stringContaining("info"),
    ]);
  });

  it("renders each level's label in the registry's color, the chip an alarm wears", () => {
    mount();
    const row = bodyRows().find((r) => r.textContent?.includes("major"))!;
    const chip = row.querySelector('[data-severity="major"]')!;
    expect(chip.textContent).toBe("Major");
    expect(chip.classList.contains("badge-average")).toBe(true);
    const disaster = bodyRows()[0].querySelector('[data-severity="disaster"]')!;
    expect(disaster.classList.contains("badge-disaster")).toBe(true);
  });

  it("shows the rank and what an alarm at the level does to its component", () => {
    mount();
    const high = bodyRows().find((r) => r.querySelector('[data-severity="high"]'))!;
    expect(within(high).getByText("40")).toBeTruthy();
    expect(within(high).getByText("outage")).toBeTruthy();
    const major = bodyRows().find((r) => r.querySelector('[data-severity="major"]'))!;
    expect(within(major).getByText("35")).toBeTruthy();
    expect(within(major).getByText("degrades")).toBeTruthy();
    expect(within(major).getByText("custom")).toBeTruthy();
    expect(within(high).getByText("official")).toBeTruthy();
  });

  it("offers no create, even to an owner: a read view", () => {
    mount();
    expect(screen.queryByText(/New severity level/i)).toBeNull();
  });
});
