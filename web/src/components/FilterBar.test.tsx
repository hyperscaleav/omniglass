import { describe, it, expect, vi } from "vitest";
import { render, fireEvent } from "@solidjs/testing-library";
import FilterBar from "./FilterBar";
import type { Chip, FilterKey } from "../lib/predicate";

// A component-level test of the staged chip combobox: it drives the real keyboard
// pipeline (type -> commit) and the chip remove affordance, asserting the chip
// array it emits. The matching logic itself is unit-tested in lib/predicate.
type Row = { name: string; type: string };
const keys: FilterKey<Row>[] = [
  { key: "name", type: "string", hint: "substring", get: (r) => r.name },
  { key: "type", type: "string", hint: "exact", get: (r) => r.type, values: () => ["codec", "display"] },
];
// A presence-capable facet (a tag key): it offers the value-less exists / absent
// operators alongside the ordinary value operators.
const withTag: FilterKey<Row>[] = [...keys, { key: "env", type: "string", hint: "tag", presence: true, get: () => "", values: () => ["prod"] }];

describe("FilterBar", () => {
  it("commits a typed key:value as a chip with the key's operator", () => {
    const onChips = vi.fn();
    const { getByRole } = render(() => <FilterBar keys={keys} rows={[]} chips={[]} onChips={onChips} />);
    const input = getByRole("combobox") as HTMLInputElement;
    fireEvent.input(input, { target: { value: "type:codec" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onChips).toHaveBeenCalledWith([{ key: "type", op: "eq", values: ["codec"] }]);
  });

  it("commits a bare token against the fallback (substring) key", () => {
    const onChips = vi.fn();
    const { getByRole } = render(() => <FilterBar keys={keys} rows={[]} chips={[]} onChips={onChips} />);
    const input = getByRole("combobox") as HTMLInputElement;
    fireEvent.input(input, { target: { value: "mic" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onChips).toHaveBeenCalledWith([{ key: "name", op: "contains", values: ["mic"] }]);
  });

  it("backspace on an empty input drops the last chip", () => {
    const onChips = vi.fn();
    const chips: Chip[] = [{ key: "type", op: "eq", values: ["codec"] }];
    const { getByRole } = render(() => <FilterBar keys={keys} rows={[]} chips={chips} onChips={onChips} />);
    fireEvent.keyDown(getByRole("combobox"), { key: "Backspace" });
    expect(onChips).toHaveBeenCalledWith([]);
  });

  it("removes a chip when its remove button is clicked", () => {
    const onChips = vi.fn();
    const chips: Chip[] = [{ key: "type", op: "eq", values: ["codec"] }];
    const { getByLabelText } = render(() => <FilterBar keys={keys} rows={[]} chips={chips} onChips={onChips} />);
    fireEvent.click(getByLabelText("remove"));
    expect(onChips).toHaveBeenCalledWith([]);
  });

  it("groups the tag facets under one top-level 'tag' entry, not each key", () => {
    const { getByRole, getAllByRole } = render(() => <FilterBar keys={withTag} rows={[]} chips={[]} onChips={vi.fn()} />);
    fireEvent.focus(getByRole("combobox"));
    const labels = getAllByRole("option").map((o) => o.textContent ?? "");
    expect(labels.some((l) => l.startsWith("tag:"))).toBe(true);
    expect(labels.every((l) => !l.startsWith("env"))).toBe(true);
  });

  it("discloses the tag keys once the tag group is chosen", () => {
    const { getByRole, getAllByRole } = render(() => <FilterBar keys={withTag} rows={[]} chips={[]} onChips={vi.fn()} />);
    fireEvent.input(getByRole("combobox"), { target: { value: "tag:" } });
    const labels = getAllByRole("option").map((o) => o.textContent ?? "");
    expect(labels.some((l) => l.startsWith("env:"))).toBe(true);
  });

  it("commits a value-less exists chip from the presence token", () => {
    const onChips = vi.fn();
    const { getByRole } = render(() => <FilterBar keys={withTag} rows={[]} chips={[]} onChips={onChips} />);
    const input = getByRole("combobox") as HTMLInputElement;
    fireEvent.input(input, { target: { value: "env:?" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onChips).toHaveBeenCalledWith([{ key: "env", op: "exists", values: [] }]);
  });

  it("commits a value-less absent chip from the presence token", () => {
    const onChips = vi.fn();
    const { getByRole } = render(() => <FilterBar keys={withTag} rows={[]} chips={[]} onChips={onChips} />);
    const input = getByRole("combobox") as HTMLInputElement;
    fireEvent.input(input, { target: { value: "env:!?" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(onChips).toHaveBeenCalledWith([{ key: "env", op: "absent", values: [] }]);
  });

  it("renders a value-less chip with no value button", () => {
    const chips: Chip[] = [{ key: "env", op: "exists", values: [] }];
    const { container } = render(() => <FilterBar keys={withTag} rows={[]} chips={chips} onChips={vi.fn()} />);
    // The only buttons on a presence chip are the operator glyph and remove (no value).
    expect(container.querySelectorAll(".font-medium").length).toBe(0);
  });
});
