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
});
