import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@solidjs/testing-library";
import TimeRangePicker from "./TimeRangePicker";
import type { TimeRange } from "../lib/time_range";

// The page's one range control (#2304): a trigger naming the range, a
// portaled panel of presets and, where the page's reads honor one, a custom
// absolute range.

afterEach(cleanup);

const mount = (range: TimeRange, allowCustom = true) => {
  const picked: TimeRange[] = [];
  const r = render(() => (
    <div data-testid="clip" style={{ overflow: "hidden" }}>
      <TimeRangePicker range={range} onChange={(x) => picked.push(x)} allowCustom={allowCustom} />
    </div>
  ));
  return Object.assign(r, { picked });
};

describe("TimeRangePicker", () => {
  it("names the range on its trigger", () => {
    mount({ kind: "preset", preset: "6h" });
    expect(screen.getByTestId("time-range-trigger").textContent).toContain("Last 6 hours");
  });

  it("floats its panel outside an overflow-clipping ancestor", async () => {
    const r = mount({ kind: "preset", preset: "24h" });
    fireEvent.click(screen.getByTestId("time-range-trigger"));
    const panel = await screen.findByTestId("time-range-panel");
    expect(r.container.querySelector('[data-testid="clip"]')!.contains(panel)).toBe(false);
  });

  it("offers every preset, marks the current one, and picks another", async () => {
    const r = mount({ kind: "preset", preset: "24h" });
    fireEvent.click(screen.getByTestId("time-range-trigger"));
    await screen.findByTestId("time-range-panel");
    for (const k of ["1h", "6h", "24h", "7d", "30d"]) expect(screen.getByTestId(`time-range-preset-${k}`)).toBeTruthy();
    expect(screen.getByTestId("time-range-preset-24h").getAttribute("aria-checked")).toBe("true");
    expect(screen.getByTestId("time-range-preset-7d").getAttribute("aria-checked")).toBe("false");
    fireEvent.click(screen.getByTestId("time-range-preset-7d"));
    expect(r.picked).toEqual([{ kind: "preset", preset: "7d" }]);
  });

  it("applies a custom absolute range read as local time", async () => {
    const r = mount({ kind: "preset", preset: "24h" });
    fireEvent.click(screen.getByTestId("time-range-trigger"));
    const from = (await screen.findByLabelText("From")) as HTMLInputElement;
    const to = screen.getByLabelText("To") as HTMLInputElement;
    fireEvent.input(from, { target: { value: "2026-11-01T08:00" } });
    fireEvent.input(to, { target: { value: "2026-11-02T08:00" } });
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));
    expect(r.picked).toEqual([{ kind: "absolute", from: new Date("2026-11-01T08:00").getTime(), to: new Date("2026-11-02T08:00").getTime() }]);
  });

  it("will not apply a custom range whose end is not after its start", async () => {
    const r = mount({ kind: "preset", preset: "24h" });
    fireEvent.click(screen.getByTestId("time-range-trigger"));
    fireEvent.input(await screen.findByLabelText("From"), { target: { value: "2026-11-02T08:00" } });
    fireEvent.input(screen.getByLabelText("To"), { target: { value: "2026-11-01T08:00" } });
    const apply = screen.getByRole("button", { name: "Apply" }) as HTMLButtonElement;
    expect(apply.disabled).toBe(true);
    expect(screen.getByText(/end after it starts/i)).toBeTruthy();
    fireEvent.click(apply);
    expect(r.picked).toEqual([]);
  });

  it("seeds the custom fields from the current absolute range", async () => {
    const from = new Date("2026-11-01T08:00").getTime();
    const to = new Date("2026-11-01T20:30").getTime();
    mount({ kind: "absolute", from, to });
    fireEvent.click(screen.getByTestId("time-range-trigger"));
    expect(((await screen.findByLabelText("From")) as HTMLInputElement).value).toBe("2026-11-01T08:00");
    expect((screen.getByLabelText("To") as HTMLInputElement).value).toBe("2026-11-01T20:30");
  });

  it("offers no custom range where the page's reads cannot honor one", async () => {
    mount({ kind: "preset", preset: "24h" }, false);
    fireEvent.click(screen.getByTestId("time-range-trigger"));
    await screen.findByTestId("time-range-panel");
    expect(screen.queryByLabelText("From")).toBeNull();
  });
});
