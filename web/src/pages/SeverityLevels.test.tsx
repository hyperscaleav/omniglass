import { describe, it, expect, afterEach, vi } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import SeverityLevels from "./SeverityLevels";
import { ME_KEY, type Me } from "../lib/auth";
import { OFFICIAL_LOCK } from "../lib/catalog";
import { MAJOR_LEVEL, SEEDED_LEVELS, seedSeverityLevels } from "../lib/testseverity";
import { componentAlarmsKey } from "../lib/alarms";
import { SYSTEM_VERDICTS_KEY, locationHealthKey, systemHealthKey } from "../lib/health";
import { FLEET_VIEW_KEY } from "../lib/fleet";
import { COMPONENTS_KEY } from "../lib/components";
import { EVENT_RULES_KEY } from "../lib/event_rules";

// The severity levels page (#921, authored since #930): the registry an event
// rule raises at, beside the rules. Each level shows its rank, its label in its
// own color (the same chip every alarm wears), whether it takes a component out,
// and its origin; worst first, the order an operator triages in. An operator
// level is created, edited and deleted in a Drawer; official levels stay
// read-only. A level's rank moves health fleet-wide, so a write is judged on the
// caller's all-scope permissions (fleet_permissions), like an event rule's. Data
// is seeded into the query cache; writes are caught at fetch.

const owner: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], fleet_permissions: [">"], grants: [{ role: "owner", scope_kind: "all" }] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], fleet_permissions: ["*:read"], grants: [{ role: "viewer", scope_kind: "all" }] };
// Owns one place, views the fleet: severity_level:* is in its flattened set,
// but writing a level needs it on an all-scope grant, so the server refuses it.
const placeOwner: Me = {
  principal: { id: "u-po", kind: "human" }, human: { username: "owner-hq" }, permissions: [">"], fleet_permissions: ["*:read"],
  grants: [{ role: "owner", scope_kind: "location", scope_id: "loc-hq" }, { role: "viewer", scope_kind: "all" }],
};

function mount(me: Me = owner, levels = [...SEEDED_LEVELS, MAJOR_LEVEL]) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], me);
  seedSeverityLevels(qc, levels);
  return Object.assign(
    render(() => (
      <QueryClientProvider client={qc}>
        <SeverityLevels />
      </QueryClientProvider>
    )),
    { qc },
  );
}

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

// Every write lands here; a GET answers from the fixtures. fail answers each
// write with that problem instead.
function catchWrites(fail?: { status: number; detail: string }) {
  const writes: { method: string; url: string; body: unknown }[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const req = input as Request;
    if (req.method !== "GET") {
      const body = req.method === "DELETE" ? undefined : await req.clone().json();
      writes.push({ method: req.method, url: req.url, body });
      if (fail) return json({ title: "Conflict", status: fail.status, detail: fail.detail }, fail.status);
      return req.method === "DELETE" ? new Response(null, { status: 204 }) : json({ ...MAJOR_LEVEL, ...(body as object) });
    }
    return json({ severity_levels: [...SEEDED_LEVELS, MAJOR_LEVEL] });
  });
  return writes;
}

const bodyRows = () => screen.getAllByRole("row").filter((r) => r.querySelector("td"));
// Open a level's blade from its row (its label also rides the color chip).
const openLevel = (name: string) => fireEvent.click(bodyRows().find((r) => r.querySelector(`[data-severity="${name}"]`))!.querySelector("td")!);
const blade = () =>
  waitFor(() => {
    const el = document.querySelectorAll("aside[data-blade]")[0];
    if (!el) throw new Error("no blade yet");
    return el as HTMLElement;
  });

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

  it("offers New severity level to a caller holding severity_level:create", () => {
    mount();
    expect(screen.getByText("New severity level")).toBeTruthy();
  });

  it("offers no create to a reader", () => {
    mount(viewer);
    expect(screen.queryByText("New severity level")).toBeNull();
  });

  it("offers no level write to a caller whose severity_level permissions stop at one place", async () => {
    mount(placeOwner);
    expect(screen.queryByText("New severity level")).toBeNull();
    openLevel("major");
    const b = await blade();
    expect(within(b).queryByText("Delete")?.closest("button")?.disabled ?? true).toBe(true);
    const editBtn = within(b).getByLabelText("Edit") as HTMLButtonElement;
    expect(editBtn.disabled).toBe(true);
    expect(editBtn.closest(".tooltip")?.getAttribute("data-tip")).toBe("Requires severity_level:update");
  });

  it("creates a level, teaching where its rank sits and what its alarms do", async () => {
    const writes = catchWrites();
    mount();
    fireEvent.click(screen.getByText("New severity level"));
    const name = await waitFor(() => screen.getByLabelText("Name") as HTMLInputElement);
    fireEvent.input(name, { target: { value: "p2" } });
    fireEvent.input(screen.getByLabelText("Label"), { target: { value: "P2" } });
    fireEvent.change(screen.getByLabelText("Color"), { target: { value: "orange" } });
    // The preview is the chip every alarm at the level will wear.
    expect(screen.getByTestId("level-preview").classList.contains("badge-average")).toBe(true);
    expect(screen.getByTestId("level-preview").textContent).toBe("P2");
    const rank = screen.getByLabelText("Rank");
    fireEvent.input(rank, { target: { value: "33" } });
    expect(screen.getByTestId("level-place").textContent).toBe("Between Average (30) and Major (35).");
    expect(screen.getByTestId("level-effect").textContent).toMatch(/only degrade a component/);
    fireEvent.input(rank, { target: { value: "45" } });
    expect(screen.getByTestId("level-place").textContent).toBe("Between High (40) and Disaster (50).");
    expect(screen.getByTestId("level-effect").textContent).toMatch(/take their component out \(rank 40\+\)/);
    const submit = screen.getByText("Create level").closest("button") as HTMLButtonElement;
    await waitFor(() => expect(submit.disabled).toBe(false));
    fireEvent.click(submit);
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0].method).toBe("POST");
    expect(writes[0].body).toEqual({ name: "p2", label: "P2", color: "orange", rank: 45 });
  });

  it("refuses a rank another level holds before it sends anything", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch");
    mount();
    fireEvent.click(screen.getByText("New severity level"));
    const name = await waitFor(() => screen.getByLabelText("Name") as HTMLInputElement);
    fireEvent.input(name, { target: { value: "p2" } });
    fireEvent.change(screen.getByLabelText("Color"), { target: { value: "red" } });
    fireEvent.input(screen.getByLabelText("Rank"), { target: { value: "40" } });
    expect(screen.getByText("High already holds rank 40; each level has its own.")).toBeTruthy();
    expect((screen.getByText("Create level").closest("button") as HTMLButtonElement).disabled).toBe(true);
    expect(fetchSpy).not.toHaveBeenCalled();
  });

  it("edits an operator level in a Drawer, sending only what changed", async () => {
    const writes = catchWrites();
    mount();
    openLevel("major");
    const b = await blade();
    fireEvent.click(within(b).getByText("Edit"));
    const label = await waitFor(() => screen.getByLabelText("Label") as HTMLInputElement);
    expect(label.value).toBe("Major");
    expect((screen.getByLabelText("Rank") as HTMLInputElement).value).toBe("35");
    fireEvent.input(label, { target: { value: "P2" } });
    fireEvent.click(screen.getByText("Save level").closest("button")!);
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0].method).toBe("PATCH");
    expect(writes[0].url).toMatch(/\/severity-levels\/major$/);
    expect(writes[0].body).toEqual({ label: "P2" });
    // Saving lands back on the level's blade.
    await blade();
  });

  // A rename moves the name every alarm chip and rule reads, and a re-rank can
  // move a component across the outage line: the alarm, health and fleet views
  // already cached are stale the moment the write lands.
  it("refreshes the alarm, health and fleet views on a save", async () => {
    catchWrites();
    const { qc } = mount();
    const stale = [
      componentAlarmsKey("display-1"),
      systemHealthKey("huddle-1"),
      locationHealthKey("hq"),
      SYSTEM_VERDICTS_KEY,
      FLEET_VIEW_KEY,
      COMPONENTS_KEY,
      EVENT_RULES_KEY,
    ];
    for (const k of stale) qc.setQueryData([...k], []);
    openLevel("major");
    const b = await blade();
    fireEvent.click(within(b).getByText("Edit"));
    const rank = await waitFor(() => screen.getByLabelText("Rank") as HTMLInputElement);
    fireEvent.input(rank, { target: { value: "45" } });
    fireEvent.click(screen.getByText("Save level").closest("button")!);
    await blade();
    for (const k of stale) expect(qc.getQueryState([...k])?.isInvalidated, k.join("/")).toBe(true);
  });

  it("keeps an official level read-only: the pair greys with the official reason", async () => {
    mount();
    openLevel("high");
    const b = await blade();
    const editBtn = within(b).getByLabelText("Edit") as HTMLButtonElement;
    expect(editBtn.disabled).toBe(true);
    expect(editBtn.closest(".tooltip")?.getAttribute("data-tip")).toBe(OFFICIAL_LOCK);
  });

  it("greys an operator level's Edit for a reader with what it needs", async () => {
    mount(viewer);
    openLevel("major");
    const b = await blade();
    const editBtn = within(b).getByLabelText("Edit") as HTMLButtonElement;
    expect(editBtn.disabled).toBe(true);
    expect(editBtn.closest(".tooltip")?.getAttribute("data-tip")).toBe("Requires severity_level:update");
  });

  it("deletes an operator level after a confirmation", async () => {
    const writes = catchWrites();
    const ask = vi.spyOn(window, "confirm").mockReturnValue(true);
    mount();
    openLevel("major");
    const b = await blade();
    fireEvent.click(within(b).getByText("Delete"));
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(ask.mock.calls[0][0]).toMatch(/an alarm or a rule still names it/);
    expect(writes[0].method).toBe("DELETE");
    expect(writes[0].url).toMatch(/\/severity-levels\/major$/);
  });

  it("says why the server refused a delete: a level in use stays", async () => {
    catchWrites({ status: 409, detail: "an alarm or an event rule still names this severity level" });
    vi.spyOn(window, "confirm").mockReturnValue(true);
    mount();
    openLevel("major");
    const b = await blade();
    fireEvent.click(within(b).getByText("Delete"));
    expect(await within(b).findByText(/still names this severity level/)).toBeTruthy();
  });
});
