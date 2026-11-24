import { describe, it, expect } from "vitest";
import { previewWords, seriesLabel, windowWords } from "./rule_preview";
import type { RulePreview } from "./expressions";

const base: RulePreview = {
  in_force_now: [], edges: [], state_unknown: [], window: "24h0m0s", window_since: "2026-11-22T00:00:00Z", window_until: "2026-11-23T00:00:00Z",
  row_cap: 5000, series_judged: 4, series_total: 4, truncated: false, sustains_ignored: true,
};
const ctx = { reads: "Temperature", condition: "Temperature above 40 °C" };
const edge = (component: string, kind: "fire" | "clear", at: string, instance = "") => ({ component, component_id: component, instance, at, kind });

describe("seriesLabel", () => {
  it("names the owner and the metric, and the endpoint when there is one", () => {
    expect(seriesLabel({ component: "Display 1", instance: "" }, "Temperature")).toBe("Display 1 · Temperature");
    expect(seriesLabel({ component: "Display 1", instance: "hdmi-1" }, "Temperature")).toBe("Display 1 · Temperature (hdmi-1)");
  });
});

describe("windowWords", () => {
  it("says a Go duration the way an operator does", () => {
    expect(windowWords("24h0m0s")).toBe("24h");
    expect(windowWords("90m0s")).toBe("90m");
    expect(windowWords("")).toBe("the window");
  });
});

describe("previewWords", () => {
  it("says what it would have done: how often it fired and cleared, and where", () => {
    const r = { ...base, edges: [edge("Display 1", "fire", "2026-11-22T10:00:00Z"), edge("Display 1", "clear", "2026-11-22T11:00:00Z"), edge("Display 7", "fire", "2026-11-22T12:00:00Z")], in_force_now: [{ component: "Display 7", component_id: "Display 7", instance: "" }] };
    const w = previewWords(r, ctx);
    expect(w.sentence).toBe("In the last 24h, Temperature above 40 °C would have fired 2 times on 2 of 4 series (Display 1, Display 7) and cleared once. Right now 1 series (Display 7) would be in force.");
  });

  it("says plainly when nothing would have happened", () => {
    expect(previewWords(base, ctx).sentence).toBe("In the last 24h, no sample met Temperature above 40 °C across the 4 series it watches, so it would not have fired.");
  });

  it("says it selects nothing when no series matches", () => {
    expect(previewWords({ ...base, series_judged: 0, series_total: 0 }, ctx).sentence).toBe("It watches no series of Temperature, so there is nothing to judge it against.");
  });

  it("says a series already in force before the window, with no edge", () => {
    const r = { ...base, in_force_now: [{ component: "Display 3", component_id: "d3", instance: "" }] };
    expect(previewWords(r, ctx).sentence).toBe("In the last 24h, Temperature above 40 °C would not have fired, but 1 series (Display 3) was already in force when the window began and stays so.");
  });

  it("says plainly that a sustain the operator typed is not applied, and what that means for the counts", () => {
    const w = previewWords(base, { ...ctx, fireSustain: "30s" });
    expect(w.sustain).toBe("You asked for it to hold for at least 30s before firing. The preview cannot apply that: it judges each sample alone, so a reading that passed within 30s would still show here as a firing. Read the counts as the most it would have done.");
  });

  it("mentions a clear sustain too, and says nothing when none was typed", () => {
    expect(previewWords(base, { ...ctx, clearSustain: "1m" }).sustain).toContain("clearing");
    expect(previewWords(base, { ...ctx, clearSustain: "1m" }).sustain).toContain("1m");
    expect(previewWords(base, ctx).sustain).toBeNull();
    expect(previewWords(base, { ...ctx, fireSustain: "0s" }).sustain).toBeNull();
  });

  it("notes series that started unknown and a row cap that cut the judgement short", () => {
    const r = { ...base, state_unknown: [{ component: "Display 2", component_id: "d2", instance: "" }], truncated: true, series_judged: 2, series_total: 4 };
    const w = previewWords(r, ctx);
    expect(w.unknown).toBe("1 series (Display 2) had no sample in the week before the window, so its first reading set its state and counted no firing.");
    expect(w.cap).toBe("Judged 2 of 4 series: the rest would pass the 5,000-sample cap.");
  });
});
