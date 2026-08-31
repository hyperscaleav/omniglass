import { afterEach, describe, expect, it, vi } from "vitest";
import {
  applyTo,
  DEFAULT_STATE,
  loadPresets,
  matches,
  PRESET_STORE,
  remove,
  savePresets,
  STOCK_PRESETS,
  upsert,
  type Preset,
  type PresetState,
} from "./presets";

// Presets (#841). A preset is a snapshot of the same object the controls write
// to, so the tests here are about what a saved view may mean, not about a
// second schema that could drift from the panel.

afterEach(() => {
  try { window.localStorage.clear(); } catch { /* nothing to clear */ }
  vi.restoreAllMocks();
});

const state = (over: Partial<PresetState> = {}): PresetState => ({ ...DEFAULT_STATE, ...over });

describe("the shipped set", () => {
  it("is named after jobs, not after the controls it moves", () => {
    expect(STOCK_PRESETS.map((p) => p.name)).toEqual([
      "Estate overview",
      "Morning triage",
      "Shape of the estate",
      "Standards audit",
      "Commissioning sweep",
    ]);
    for (const p of STOCK_PRESETS) expect(p.why.length).toBeGreaterThan(0);
  });

  it("carries only ways of looking, never a scope", () => {
    // The tripwire: a preset that named a subject would have made this an
    // explorer that is quietly a dashboard.
    for (const p of STOCK_PRESETS) {
      expect(p.state.node).toBeNull();
      expect(Object.keys(p.state).sort()).toEqual(Object.keys(DEFAULT_STATE).sort());
    }
  });
});

describe("matching", () => {
  it("marks a preset active when the controls agree with it", () => {
    expect(matches(state(), STOCK_PRESETS[0])).toBe(true);
    expect(matches(state({ renderer: "mosaic" }), STOCK_PRESETS[0])).toBe(false);
    expect(matches(state({ renderer: "mosaic" }), STOCK_PRESETS[2])).toBe(true);
  });

  it("ignores where the operator has drilled", () => {
    // A preset is a way of looking. Drilling somewhere does not stop you
    // looking that way, so the chip must not go dark for it.
    expect(matches(state({ node: "somewhere" }), STOCK_PRESETS[0])).toBe(true);
  });
});

describe("applying", () => {
  it("reproduces the state it was saved from", () => {
    const saved: Preset = { name: "mine", why: "", state: state({ renderer: "matrix", density: "roomy" }) };
    expect(applyTo(saved, () => true)).toEqual(state({ renderer: "matrix", density: "roomy" }));
  });

  it("falls back to the fleet when the saved node is gone", () => {
    // Saved against one estate, applied to another, or applied after the
    // location was deleted. Landing on a blank page is worse than landing
    // somewhere real.
    const saved: Preset = { name: "mine", why: "", state: state({ node: "vanished" }) };
    expect(applyTo(saved, () => false).node).toBeNull();
  });

  it("keeps a node that still exists", () => {
    const saved: Preset = { name: "mine", why: "", state: state({ node: "here" }) };
    expect(applyTo(saved, (id) => id === "here").node).toBe("here");
  });

  it("fills in a field a preset saved before that control existed", () => {
    const old = { name: "old", why: "", state: { renderer: "bands" } as unknown as PresetState };
    expect(applyTo(old, () => true)).toEqual(state({ renderer: "bands" }));
  });
});

describe("storage is a convenience, never a dependency", () => {
  it("round-trips what it saved", () => {
    const list = upsert([], "Mine", state({ renderer: "mosaic" }));
    expect(savePresets(list)).toBe(true);
    expect(loadPresets().map((p) => p.name)).toEqual(["Mine"]);
    expect(loadPresets()[0].state.renderer).toBe("mosaic");
  });

  it("never persists the shipped set, which the code already carries", () => {
    savePresets([...STOCK_PRESETS, ...upsert([], "Mine", state())]);
    expect(loadPresets().map((p) => p.name)).toEqual(["Mine"]);
  });

  it("survives storage that throws, leaving the shipped presets working", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => { throw new Error("blocked"); });
    expect(loadPresets()).toEqual([]);
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => { throw new Error("quota"); });
    expect(savePresets(upsert([], "Mine", state()))).toBe(false);
  });

  it("ignores stored junk rather than trusting it", () => {
    window.localStorage.setItem(PRESET_STORE, "not json");
    expect(loadPresets()).toEqual([]);
    window.localStorage.setItem(PRESET_STORE, JSON.stringify({ nope: true }));
    expect(loadPresets()).toEqual([]);
    window.localStorage.setItem(PRESET_STORE, JSON.stringify([{ name: 1 }, null, { state: {} }]));
    expect(loadPresets()).toEqual([]);
  });

  it("marks everything it loads as not stock, whatever the stored flag claimed", () => {
    window.localStorage.setItem(PRESET_STORE, JSON.stringify([{ name: "Sneaky", why: "", stock: true, state: {} }]));
    expect(loadPresets()[0].stock).toBe(false);
  });
});

describe("editing the saved set", () => {
  it("adds, then replaces on the same name rather than duplicating", () => {
    let list = upsert([], "Mine", state({ renderer: "bands" }));
    list = upsert(list, "Mine", state({ renderer: "matrix" }));
    expect(list).toHaveLength(1);
    expect(list[0].state.renderer).toBe("matrix");
  });

  it("refuses an empty or whitespace name", () => {
    expect(upsert([], "   ", state())).toEqual([]);
    expect(upsert([], "", state())).toEqual([]);
  });

  it("trims and caps a name so it stays usable as a chip", () => {
    const list = upsert([], `  ${"x".repeat(80)}  `, state());
    expect(list[0].name).toHaveLength(40);
  });

  it("forgets one by name and leaves the rest", () => {
    let list = upsert([], "A", state());
    list = upsert(list, "B", state());
    expect(remove(list, "A").map((p) => p.name)).toEqual(["B"]);
  });
});
