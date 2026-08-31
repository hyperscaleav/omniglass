import type { Density } from "../components/DotField";
import type { LabelMode } from "./view_budgets";

// Presets (#841): a saved way of looking, named after the job it serves.
//
// The rule that keeps this honest is that a preset is a snapshot of THE SAME
// OBJECT the controls write to. There is no separate preset schema to drift
// from the panel, so a saved view can never mean something the controls cannot
// produce, and adding a control adds it to every preset for free.
//
// What a preset is NOT is a saved scope. Every field below changes how the
// estate is drawn or which live state is filtered; none of them names a subject
// to include or exclude. That line is what keeps this an explorer rather than a
// dashboard with no owner, no permission and no audit row: the moment somebody
// wants to share one, or scope one to a named part of the estate, it has become
// a widget and belongs to a dashboard instead.

export type RendererKey = "cards" | "bands" | "mosaic" | "matrix";

export type PresetState = {
  renderer: RendererKey;
  density: Density;
  labelMode: LabelMode;
  roomBox: boolean;
  sort: "worst" | "name";
  attentionOnly: boolean;
  // Where the operator was standing. A node is an address, not a scope: it says
  // where to look from, and the estate it names may not be the one a preset is
  // later applied to.
  node: string | null;
};

export type Preset = { name: string; why: string; state: PresetState; stock?: boolean };

export const DEFAULT_STATE: PresetState = {
  renderer: "cards",
  density: "compact",
  labelMode: "auto",
  roomBox: true,
  sort: "worst",
  attentionOnly: false,
  node: null,
};

// The shipped set. Each is a job somebody actually does, not a combination of
// settings that happens to look nice, which is why they are named after the
// job rather than after the controls they move.
export const STOCK_PRESETS: Preset[] = [
  {
    name: "Estate overview",
    why: "what the fleet looks like on arrival",
    stock: true,
    state: { ...DEFAULT_STATE },
  },
  {
    name: "Morning triage",
    why: "only what is broken, worst first, named",
    stock: true,
    state: { ...DEFAULT_STATE, density: "cozy", labelMode: "always", attentionOnly: true },
  },
  {
    name: "Shape of the estate",
    why: "which parts carry the weight",
    stock: true,
    state: { ...DEFAULT_STATE, renderer: "mosaic" },
  },
  {
    name: "Standards audit",
    why: "how one standard is doing everywhere",
    stock: true,
    state: { ...DEFAULT_STATE, renderer: "matrix" },
  },
  {
    name: "Commissioning sweep",
    why: "room by room, every name and box on",
    stock: true,
    state: { ...DEFAULT_STATE, density: "roomy", labelMode: "always", roomBox: true, sort: "name" },
  },
];

export const PRESET_STORE = "explore-presets";

// Storage is a convenience, never a dependency: a private window, blocked site
// data or a quota error must leave the shipped presets working rather than
// taking the page down with it.
export function loadPresets(): Preset[] {
  try {
    const raw = window.localStorage.getItem(PRESET_STORE);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) return [];
    return parsed
      .filter((p): p is Preset => !!p && typeof (p as Preset).name === "string" && !!(p as Preset).state)
      .map((p) => ({ ...p, stock: false, state: { ...DEFAULT_STATE, ...p.state } }));
  } catch {
    return [];
  }
}

export function savePresets(list: Preset[]): boolean {
  try {
    window.localStorage.setItem(PRESET_STORE, JSON.stringify(list.filter((p) => !p.stock)));
    return true;
  } catch {
    return false;
  }
}

// matches decides which chip reads as active. The drilled node is deliberately
// NOT compared: a preset describes a way of looking, and an operator who has
// drilled somewhere is still looking that way.
export function matches(state: PresetState, preset: Preset): boolean {
  const keys: Array<keyof PresetState> = ["renderer", "density", "labelMode", "roomBox", "sort", "attentionOnly"];
  return keys.every((k) => state[k] === preset.state[k]);
}

// applyTo resolves a saved state against the estate in front of the operator.
// A preset saved against one fleet and applied where its node no longer exists
// falls back to the fleet level, because landing on a blank page is worse than
// landing somewhere real.
export function applyTo(preset: Preset, nodeExists: (id: string) => boolean): PresetState {
  const state = { ...DEFAULT_STATE, ...preset.state };
  if (state.node && !nodeExists(state.node)) return { ...state, node: null };
  return state;
}

// A name has to be usable as a chip and as a key, so it is trimmed, capped, and
// refused when empty. Saving over an existing name replaces it, which is what
// somebody re-saving a view they already named means.
export function upsert(list: Preset[], name: string, state: PresetState): Preset[] {
  const clean = name.trim().slice(0, 40);
  if (!clean) return list;
  const without = list.filter((p) => p.name !== clean);
  return [...without, { name: clean, why: "saved in this browser", state: { ...state }, stock: false }];
}

export function remove(list: Preset[], name: string): Preset[] {
  return list.filter((p) => p.name !== name);
}
