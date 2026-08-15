import { describe, it, expect } from "vitest";
import { REQUIRED_FACES, fontFailures } from "../e2e/fontguard.mjs";

// The capture-side half of #775. Self-hosting the faces removes the race, but the
// capture must also refuse to WRITE a shot rendered in the wrong face: that is the
// property that failed, silently, on main. A fallback-font PNG was written as
// though it were real and only the freshness gate noticed, three steps downstream,
// where it read as UI drift.
//
// The decision is pure so it can be tested without a browser: docs-shots.mjs
// collects what the page reports (which faces the document declares, which of them
// loaded, which errored) plus any font request that failed, and this turns that
// into the reason to abort.
const face = (family: string, weight: string, style: "normal" | "italic", over = {}) => ({
  family,
  weight,
  style,
  declared: 6, // one @font-face per unicode-range split (latin, latin-ext, ...)
  loaded: false,
  errored: false,
  ...over,
});

// A healthy capture: every face declared, and the weights the page actually paints
// have loaded. The weights it does not paint stay unloaded, which is normal.
const healthy = () =>
  REQUIRED_FACES.map((f) =>
    face(f.family, f.weight, f.style, { loaded: f.weight === "400" && f.style === "normal" }),
  );

describe("capture font guard", () => {
  it("passes when both families rendered from their own declared files", () => {
    expect(fontFailures({ faces: healthy(), failedRequests: [] })).toEqual([]);
  });

  it("does not fault a declared weight the page never paints", () => {
    // Only 400 loads in `healthy()`; the other seven faces are declared and idle.
    // A per-face load requirement would fail honest shots, so the render check is
    // per family.
    expect(healthy().filter((f) => f.loaded)).toHaveLength(2);
    expect(fontFailures({ faces: healthy(), failedRequests: [] })).toEqual([]);
  });

  it("fails when a family painted in fallback metrics, naming the family", () => {
    const faces = healthy().map((f) =>
      f.family === "IBM Plex Sans" ? { ...f, loaded: false } : f,
    );
    const failures = fontFailures({ faces, failedRequests: [] });
    expect(failures).toHaveLength(1);
    expect(failures[0]).toContain("IBM Plex Sans");
    expect(failures[0]).toContain("fallback");
  });

  it("fails when the whole stylesheet is missing, which is what #775 looked like", () => {
    const failures = fontFailures({
      faces: REQUIRED_FACES.map((f) => face(f.family, f.weight, f.style, { declared: 0 })),
      failedRequests: [],
    });
    // One per undeclared face, and no duplicate per-family render complaint on top.
    expect(failures).toHaveLength(9);
    expect(failures.every((f) => f.startsWith("no @font-face declares"))).toBe(true);
  });

  it("fails when a face is not declared at all", () => {
    // document.fonts.check() answers TRUE for a family nothing declares (no face
    // matched, so no face is unloaded), which is exactly the state a dropped
    // @font-face or a 404 font path produces. Declaredness has to be asserted
    // separately or a console with no webfonts at all reads as a clean capture.
    const faces = healthy().map((f) =>
      f.family === "JetBrains Mono" && f.weight === "600" ? { ...f, declared: 0 } : f,
    );
    const failures = fontFailures({ faces, failedRequests: [] });
    expect(failures).toHaveLength(1);
    expect(failures[0]).toBe("no @font-face declares JetBrains Mono 600");
  });

  it("fails when a declared face errored", () => {
    const faces = healthy().map((f) =>
      f.family === "IBM Plex Sans" && f.style === "italic" ? { ...f, errored: true } : f,
    );
    const failures = fontFailures({ faces, failedRequests: [] });
    expect(failures).toHaveLength(1);
    expect(failures[0]).toContain("IBM Plex Sans 400 italic");
  });

  it("fails when a font file failed in flight, naming the request", () => {
    const failures = fontFailures({
      faces: healthy(),
      failedRequests: ["/web/fonts/ibm-plex-sans-latin-400.woff2 (404)"],
    });
    expect(failures).toHaveLength(1);
    expect(failures[0]).toContain("ibm-plex-sans-latin-400.woff2");
  });

  it("requires the same nine faces the type system declares", () => {
    expect(REQUIRED_FACES).toHaveLength(9);
    expect(REQUIRED_FACES.filter((f) => f.style === "italic")).toHaveLength(1);
    expect(new Set(REQUIRED_FACES.map((f) => f.family))).toEqual(
      new Set(["IBM Plex Sans", "JetBrains Mono"]),
    );
  });
});
