import { test, expect } from "@playwright/test";
import { HOUSE_VIEWPORT, parseViewport } from "./shotviewport.mjs";

// shot.mjs --viewport WxH (#845): two positive integers or nothing.
test.describe("parseViewport", () => {
  test("reads WxH", () => {
    expect(parseViewport("1280x800")).toEqual({ width: 1280, height: 800 });
  });
  test("the house viewport is 1320x860", () => {
    expect(HOUSE_VIEWPORT).toEqual({ width: 1320, height: 860 });
  });
  for (const bad of ["", "1280", "1280x", "x800", "0x800", "1280x0", "-1x800", "1.5x800", "1280X800", "1280x800x2", " 1280x800", "axb"]) {
    test(`rejects ${JSON.stringify(bad)}`, () => {
      expect(parseViewport(bad)).toBeNull();
    });
  }
  test("rejects a missing value", () => {
    expect(parseViewport(undefined)).toBeNull();
  });
});
