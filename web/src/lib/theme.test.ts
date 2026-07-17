import { describe, it, expect } from "vitest";
import { themeFromMe } from "./theme";

describe("themeFromMe", () => {
  it("prefers the effective ui.theme, falls back to dark", () => {
    expect(themeFromMe({ values: { ui: { theme: "omniglass-light" } } })).toBe("light");
    expect(themeFromMe({ values: { ui: { theme: "omniglass-dark" } } })).toBe("dark");
    expect(themeFromMe({ values: {} })).toBe("dark");
    expect(themeFromMe({})).toBe("dark");
  });
});
