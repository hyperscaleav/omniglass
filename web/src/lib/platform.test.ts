import { describe, it, expect } from "vitest";
import { isMac, shortcutModifier, shortcutHint } from "./platform";

describe("isMac", () => {
  it("is true for the userAgentData and navigator.platform mac strings", () => {
    expect(isMac("macOS")).toBe(true);
    expect(isMac("MacIntel")).toBe(true);
  });

  it("is false for Windows and Linux", () => {
    expect(isMac("Windows")).toBe(false);
    expect(isMac("Win32")).toBe(false);
    expect(isMac("Linux x86_64")).toBe(false);
    expect(isMac("")).toBe(false);
  });
});

describe("shortcutModifier", () => {
  it("is the command glyph on mac and Ctrl elsewhere", () => {
    expect(shortcutModifier("MacIntel")).toBe("⌘");
    expect(shortcutModifier("Win32")).toBe("Ctrl");
  });
});

describe("shortcutHint", () => {
  it("reads as the native combo per platform", () => {
    expect(shortcutHint("MacIntel", "K")).toBe("⌘K");
    expect(shortcutHint("Win32", "K")).toBe("Ctrl+K");
  });
});
