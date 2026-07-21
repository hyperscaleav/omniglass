import { describe, it, expect } from "vitest";
import { splitCapabilities } from "./component_capabilities";

// The resolved set the API returns is flat, so the origins are derived against
// what the product declares: resolved minus product is what the component added,
// product minus resolved is what it suppressed. This is the whole of the panel's
// grouping logic, and it is pure, so it is tested without a server.
describe("splitCapabilities", () => {
  it("labels an inherited capability by its product", () => {
    expect(splitCapabilities(["touch-panel"], ["touch-panel"])).toEqual([{ id: "touch-panel", origin: "product" }]);
  });

  it("labels a capability the product does not declare as the component's own", () => {
    expect(splitCapabilities(["microphone"], ["touch-panel"])).toEqual([
      { id: "microphone", origin: "component" },
      { id: "touch-panel", origin: "suppressed" },
    ]);
  });

  // A suppressed capability is missing from the resolved set by definition, so it
  // must be carried through rather than dropped: it is the only handle the
  // operator has to restore it.
  it("keeps what the product declares and the component removed, as suppressed", () => {
    const lines = splitCapabilities(["touch-panel"], ["touch-panel", "speaker"]);
    expect(lines.find((l) => l.id === "speaker")).toEqual({ id: "speaker", origin: "suppressed" });
  });

  it("reads every capability as the component's own when the product declares none", () => {
    expect(splitCapabilities(["microphone", "speaker"], []).every((l) => l.origin === "component")).toBe(true);
  });

  it("sorts by id, so the list is stable across a refetch", () => {
    expect(splitCapabilities(["speaker", "microphone"], ["camera"]).map((l) => l.id)).toEqual([
      "camera",
      "microphone",
      "speaker",
    ]);
  });

  it("is empty when nothing is resolved and nothing is declared", () => {
    expect(splitCapabilities([], [])).toEqual([]);
  });
});
