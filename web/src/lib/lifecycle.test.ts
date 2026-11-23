import { describe, it, expect } from "vitest";
import { isIsoDate, formatLifecycleDate, formatLifecycleCost, lifecycleRows, LIFECYCLE_DATES } from "./lifecycle";
import { parseInput } from "./variables";

describe("lifecycle", () => {
  it("accepts only a real ISO-8601 calendar date", () => {
    expect(isIsoDate("2030-06-30")).toBe(true);
    expect(isIsoDate("2030-02-30")).toBe(false);
    expect(isIsoDate("next tuesday")).toBe(false);
    expect(isIsoDate("30/06/2030")).toBe(false);
  });

  it("the date input refuses a non-date and passes a date through", () => {
    expect(parseInput("string", "2030-06-30", "end-of-life")).toBe("2030-06-30");
    expect(() => parseInput("string", "soon", "end-of-life")).toThrow(/date/);
    // a non-lifecycle string is untouched
    expect(parseInput("string", "soon", "serial-number")).toBe("soon");
  });

  it("formats dates as dates and cost as currency", () => {
    expect(formatLifecycleDate("2030-06-30")).toBe("Jun 30, 2030");
    expect(formatLifecycleCost(1234.5, "USD")).toBe("$1,234.50");
  });

  it("renders the lifecycle group from properties and metrics, skipping the unset", () => {
    const rows = lifecycleRows(
      [
        { property_type_name: "end-of-life", label: "End of Life", value: "2030-06-30" },
        { property_type_name: "install-date", label: "Install Date", value: null },
        { property_type_name: "serial-number", label: "Serial", value: "X" },
      ],
      [
        { metric_type_name: "replacement-cost", label: "Replacement Cost", value: 1200, unit: "USD" },
        { metric_type_name: "expected-life-years", label: "Expected Life", value: 7, unit: "years" },
      ],
    );
    expect(rows.map((r) => [r.name, r.text])).toEqual([
      ["end-of-life", "Jun 30, 2030"],
      ["replacement-cost", "$1,200.00"],
      ["expected-life-years", "7 years"],
    ]);
    expect(LIFECYCLE_DATES).toContain("warranty-end");
  });
});
