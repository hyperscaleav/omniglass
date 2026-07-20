import { describe, it, expect } from "vitest";
import {
  parseCombo,
  matchCombo,
  chordFromEvent,
  isEditableTarget,
  allowsWhileTyping,
  resolveBinding,
  keybindingsFromMe,
  keybindingLabel,
  DEFAULT_KEYBINDINGS,
  type Scope,
} from "./keymap";

describe("parseCombo", () => {
  it("resolves the mod token to meta on mac and ctrl elsewhere", () => {
    expect(parseCombo("mod+k", true)).toEqual({ key: "k", meta: true, ctrl: false, alt: false, shift: false });
    expect(parseCombo("mod+k", false)).toEqual({ key: "k", meta: false, ctrl: true, alt: false, shift: false });
  });

  it("parses a bare key with no modifiers", () => {
    expect(parseCombo("d", false)).toEqual({ key: "d", meta: false, ctrl: false, alt: false, shift: false });
  });

  it("normalizes a named key to lower case (Escape)", () => {
    expect(parseCombo("Escape", false)).toEqual({ key: "escape", meta: false, ctrl: false, alt: false, shift: false });
  });

  it("parses explicit shift and symbol keys", () => {
    expect(parseCombo("shift+3", false)).toEqual({ key: "3", meta: false, ctrl: false, alt: false, shift: true });
    expect(parseCombo("#", false)).toMatchObject({ key: "#", shift: false });
    expect(parseCombo("?", false)).toMatchObject({ key: "?" });
  });

  it("parses the explicit control modifiers regardless of platform", () => {
    expect(parseCombo("ctrl+alt+x", true)).toEqual({ key: "x", meta: false, ctrl: true, alt: true, shift: false });
  });
});

describe("matchCombo", () => {
  const macPalette = parseCombo("mod+k", true);

  it("matches the platform-resolved modifier exactly", () => {
    expect(matchCombo({ key: "k", meta: true, ctrl: false, alt: false, shift: false }, macPalette)).toBe(true);
    // Ctrl+K on mac must NOT trigger the meta-resolved mod+k.
    expect(matchCombo({ key: "k", meta: false, ctrl: true, alt: false, shift: false }, macPalette)).toBe(false);
  });

  it("requires an unspecified control modifier to be absent", () => {
    const d = parseCombo("d", false);
    expect(matchCombo({ key: "d", meta: false, ctrl: false, alt: false, shift: false }, d)).toBe(true);
    // A bare binding must not fire while a control modifier is held.
    expect(matchCombo({ key: "d", meta: false, ctrl: true, alt: false, shift: false }, d)).toBe(false);
  });

  it("permits shift when the combo does not name it (symbols carry their own shift)", () => {
    const help = parseCombo("?", false);
    expect(matchCombo({ key: "?", meta: false, ctrl: false, alt: false, shift: true }, help)).toBe(true);
  });

  it("matches the key case-insensitively", () => {
    const esc = parseCombo("Escape", false);
    expect(matchCombo({ key: "escape", meta: false, ctrl: false, alt: false, shift: false }, esc)).toBe(true);
  });

  it("does not fire a bare letter binding while shift is held (Shift+D is not d)", () => {
    const d = parseCombo("d", false);
    expect(matchCombo({ key: "d", meta: false, ctrl: false, alt: false, shift: true }, d)).toBe(false);
    // A symbol binding still matches with shift (the symbol carries it).
    const hash = parseCombo("#", false);
    expect(matchCombo({ key: "#", meta: false, ctrl: false, alt: false, shift: true }, hash)).toBe(true);
  });
});

describe("chordFromEvent", () => {
  it("lower-cases the key and reads the modifier flags", () => {
    const chord = chordFromEvent({ key: "K", metaKey: true, ctrlKey: false, altKey: false, shiftKey: true } as KeyboardEvent);
    expect(chord).toEqual({ key: "k", meta: true, ctrl: false, alt: false, shift: true });
  });
});

describe("isEditableTarget", () => {
  it("is true for text inputs, textareas, selects, and contenteditable", () => {
    expect(isEditableTarget(document.createElement("input"))).toBe(true);
    expect(isEditableTarget(document.createElement("textarea"))).toBe(true);
    expect(isEditableTarget(document.createElement("select"))).toBe(true);
    const ce = document.createElement("div");
    ce.setAttribute("contenteditable", "true");
    expect(isEditableTarget(ce)).toBe(true);
  });

  it("is false for a plain element and for null", () => {
    expect(isEditableTarget(document.createElement("div"))).toBe(false);
    expect(isEditableTarget(null)).toBe(false);
  });
});

describe("allowsWhileTyping", () => {
  it("blocks bare keys but allows modified combos and Escape", () => {
    expect(allowsWhileTyping(parseCombo("d", false))).toBe(false);
    expect(allowsWhileTyping(parseCombo("?", false))).toBe(false);
    expect(allowsWhileTyping(parseCombo("mod+k", false))).toBe(true);
    expect(allowsWhileTyping(parseCombo("Escape", false))).toBe(true);
  });
});

describe("catalog-derived defaults and labels", () => {
  it("derives DEFAULT_KEYBINDINGS from the generated catalog, including help", () => {
    expect(DEFAULT_KEYBINDINGS.open_detail).toBe("d");
    expect(DEFAULT_KEYBINDINGS.command_palette).toBe("mod+k");
    expect(DEFAULT_KEYBINDINGS.help).toBe("?");
  });

  it("keybindingLabel reads the human description from the catalog", () => {
    expect(keybindingLabel("command_palette")).toBe("Open the command palette");
    expect(keybindingLabel("help")).toBe("Show keyboard shortcuts");
    // Unknown action falls back to its id.
    expect(keybindingLabel("nope")).toBe("nope");
  });
});

describe("keybindingsFromMe", () => {
  it("falls back to the code defaults when settings are not loaded", () => {
    expect(keybindingsFromMe(undefined)).toEqual(DEFAULT_KEYBINDINGS);
  });

  it("overlays the keybindings namespace from /settings/me over the defaults", () => {
    const merged = keybindingsFromMe({ values: { keybindings: { open_detail: "o", command_palette: "mod+p" } } });
    expect(merged.open_detail).toBe("o");
    expect(merged.command_palette).toBe("mod+p");
    // Unset keys keep the default.
    expect(merged.close_blade).toBe(DEFAULT_KEYBINDINGS.close_blade);
  });

  it("ignores non-string or empty override values", () => {
    const merged = keybindingsFromMe({ values: { keybindings: { open_detail: "", open_edit: 42 as unknown as string } } });
    expect(merged.open_detail).toBe(DEFAULT_KEYBINDINGS.open_detail);
    expect(merged.open_edit).toBe(DEFAULT_KEYBINDINGS.open_edit);
  });
});

describe("resolveBinding", () => {
  const run = () => {};
  const scopes = (): Scope[] => [
    { name: "global", priority: 10, bindings: [{ action: "palette", label: "Palette", combo: parseCombo("mod+k", true), run }] },
    { name: "blade", priority: 30, bindings: [{ action: "close", label: "Close", combo: parseCombo("Escape", false), run }] },
  ];

  it("returns the binding that claims the chord", () => {
    const b = resolveBinding(scopes(), { key: "escape", meta: false, ctrl: false, alt: false, shift: false }, false);
    expect(b?.action).toBe("close");
  });

  it("returns null when nothing claims the chord", () => {
    expect(resolveBinding(scopes(), { key: "z", meta: false, ctrl: false, alt: false, shift: false }, false)).toBeNull();
  });

  it("resolves the higher-priority scope first when two scopes bind the same combo", () => {
    const both: Scope[] = [
      { name: "list", priority: 20, bindings: [{ action: "list-d", label: "d", combo: parseCombo("d", false), run }] },
      { name: "blade", priority: 30, bindings: [{ action: "blade-d", label: "d", combo: parseCombo("d", false), run }] },
    ];
    expect(resolveBinding(both, { key: "d", meta: false, ctrl: false, alt: false, shift: false }, false)?.action).toBe("blade-d");
  });

  it("suppresses bare-key bindings while typing but keeps modified combos", () => {
    const s = scopes();
    // A bare 'd' list binding is suppressed in an editable target.
    s.push({ name: "list", priority: 20, bindings: [{ action: "list-d", label: "d", combo: parseCombo("d", false), run }] });
    expect(resolveBinding(s, { key: "d", meta: false, ctrl: false, alt: false, shift: false }, true)).toBeNull();
    // mod+k still resolves while typing.
    expect(resolveBinding(s, { key: "k", meta: true, ctrl: false, alt: false, shift: false }, true)?.action).toBe("palette");
  });
});
