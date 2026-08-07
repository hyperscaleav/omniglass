import { describe, it, expect } from "vitest";
import { resolveIcon, Landmark, Building, Layers, DoorOpen, MapPin, Monitor, Mic, Box, Server } from "./icons";

// resolveIcon is the seam that lets the API name a type's glyph: a known key maps
// to its component, and anything unknown or missing falls back to MapPin so the
// tree stays renderable if the registry ships a key the console does not know yet.
describe("resolveIcon", () => {
  it("maps each seeded location_type icon key to its glyph", () => {
    expect(resolveIcon("landmark")).toBe(Landmark);
    expect(resolveIcon("building")).toBe(Building);
    expect(resolveIcon("layers")).toBe(Layers);
    expect(resolveIcon("door-open")).toBe(DoorOpen);
    expect(resolveIcon("map-pin")).toBe(MapPin);
  });

  // The device-class glyphs the seeded component_type tree names
  // (internal/seed/component_types.yaml): a sample of roots, one generic.
  it("maps each seeded component_type icon key to its glyph", () => {
    expect(resolveIcon("monitor")).toBe(Monitor);
    expect(resolveIcon("mic")).toBe(Mic);
    expect(resolveIcon("box")).toBe(Box);
    expect(resolveIcon("server")).toBe(Server);
  });

  it("falls back to MapPin for an unknown, empty, or missing key", () => {
    expect(resolveIcon("no-such-icon")).toBe(MapPin);
    expect(resolveIcon("")).toBe(MapPin);
    expect(resolveIcon(undefined)).toBe(MapPin);
    expect(resolveIcon(null)).toBe(MapPin);
  });
});
