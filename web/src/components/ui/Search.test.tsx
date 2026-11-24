import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import Search, { type SearchSection } from "./Search";

const SECTIONS: SearchSection[] = [
  { label: "Pages", options: [{ value: "p1", label: "Auditorium", detail: "HQ" }] },
  { label: "Components", options: [{ value: "c1", label: "panel-1", detail: "HQ / Auditorium" }, { value: "c2", label: "panel-1", detail: "HQ / Boardroom" }] },
];

function mount(over: Partial<Parameters<typeof Search>[0]> = {}) {
  const onPick = vi.fn();
  const onEscape = vi.fn();
  const onQuery = vi.fn();
  render(() => <Search aria-label="Find" sections={SECTIONS} onQuery={onQuery} onPick={onPick} onEscape={onEscape} {...over} />);
  return { onPick, onEscape, onQuery };
}
const field = () => screen.getByRole("combobox", { name: "Find" });
const highlighted = () => document.querySelector("[role=option][data-highlighted]")?.textContent ?? null;

describe("Search", () => {
  it("draws the groups' names on their first rows and tells same-named rows apart by detail", async () => {
    mount();
    const list = await screen.findByRole("listbox");
    expect([...list.querySelectorAll("[data-group-start]")].map((e) => e.getAttribute("data-group-start"))).toEqual(["Pages", "Components"]);
    expect(within(list).getAllByRole("option").map((o) => o.textContent)).toEqual([
      "Pages: AuditoriumHQ",
      "Components: panel-1HQ / Auditorium",
      "Components: panel-1HQ / Boardroom",
    ]);
  });

  it("marks the first row as the default, and Enter with nothing highlighted picks it", async () => {
    const { onPick } = mount();
    await screen.findByRole("listbox");
    expect(document.querySelector("[data-first]")?.textContent).toContain("Auditorium");
    field().focus();
    fireEvent.keyDown(field(), { key: "Enter" });
    expect(onPick).toHaveBeenCalledWith(expect.objectContaining({ value: "p1" }));
  });

  it("moves with the arrows and Enter picks the highlighted row", async () => {
    const { onPick } = mount();
    await screen.findByRole("listbox");
    field().focus();
    fireEvent.keyDown(field(), { key: "ArrowDown" });
    await waitFor(() => expect(highlighted()).toContain("Auditorium"));
    fireEvent.keyDown(field(), { key: "ArrowDown" });
    fireEvent.keyDown(field(), { key: "ArrowDown" });
    await waitFor(() => expect(highlighted()).toContain("Boardroom"));
    fireEvent.keyDown(field(), { key: "Enter" });
    expect(onPick).toHaveBeenCalledTimes(1);
    expect(onPick).toHaveBeenCalledWith(expect.objectContaining({ value: "c2" }));
  });

  it("calls onEscape on Escape and keeps the focus in the field", async () => {
    const { onEscape } = mount();
    await screen.findByRole("listbox");
    field().focus();
    fireEvent.keyDown(field(), { key: "Escape" });
    await waitFor(() => expect(onEscape).toHaveBeenCalled());
    expect(document.activeElement).toBe(field());
  });

  it("reports what is typed, filters nothing itself, and shows a loading row", async () => {
    const { onQuery } = mount({ loading: true });
    field().focus();
    fireEvent.input(field(), { target: { value: "zzz" } });
    await waitFor(() => expect(onQuery).toHaveBeenCalledWith("zzz"));
    expect(screen.getAllByRole("option")).toHaveLength(3);
    expect(screen.getByRole("status").textContent).toContain("Searching");
  });

  it("says so when there is nothing to show", async () => {
    mount({ sections: [], emptyText: "Nothing found" });
    expect(await screen.findByText("Nothing found")).toBeTruthy();
  });
});
