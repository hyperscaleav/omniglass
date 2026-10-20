import { describe, it, expect } from "vitest";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import {
  astroBuildArgs,
  collectRenderInputs,
  renderDigest,
} from "../../docs/scripts/lib/render-stamp.mjs";

// The docs build forces Astro's render cache exactly when an input the
// walkthrough embed reads, and Astro cannot see, has changed since the cached
// render (#937): every copied walkthrough.json, which files sit beside it (the
// video renders only when present), and the plugin sources that render it.
const input = (name: string, bytes: string) => ({ name, bytes });

describe("renderDigest", () => {
  it("is the same for the same inputs in any order", () => {
    const a = [input("x/walkthrough.json", "{}"), input("y/walkthrough.json", "[]")];
    expect(renderDigest(a)).toBe(renderDigest([...a].reverse()));
  });

  it("changes when any input's bytes change", () => {
    const before = renderDigest([input("x/walkthrough.json", '{"title":"a"}')]);
    expect(renderDigest([input("x/walkthrough.json", '{"title":"b"}')])).not.toBe(before);
  });

  it("changes when an input is added, removed or renamed", () => {
    const one = [input("x/walkthrough.json", "{}")];
    const base = renderDigest(one);
    expect(renderDigest([...one, input("y/walkthrough.json", "{}")])).not.toBe(base);
    expect(renderDigest([])).not.toBe(base);
    expect(renderDigest([input("z/walkthrough.json", "{}")])).not.toBe(base);
  });

  it("frames each input, so moving bytes across the name boundary is a change", () => {
    expect(renderDigest([input("ab", "c")])).not.toBe(renderDigest([input("a", "bc")]));
    expect(renderDigest([input("a", "b"), input("c", "")])).not.toBe(renderDigest([input("a", ""), input("b", "c")]));
  });

  it("hashes bytes and strings alike", () => {
    expect(renderDigest([{ name: "a", bytes: new TextEncoder().encode("é") }])).toBe(
      renderDigest([input("a", "é")]),
    );
  });
});

describe("astroBuildArgs", () => {
  it("builds from the cache when the stamp matches the digest", () => {
    expect(astroBuildArgs("abc", "abc")).toEqual(["build"]);
    expect(astroBuildArgs("abc", "abc\n")).toEqual(["build"]);
  });

  it("forces the build when the stamp differs or there is none", () => {
    expect(astroBuildArgs("abc", "abd")).toEqual(["build", "--force"]);
    expect(astroBuildArgs("abc", null)).toEqual(["build", "--force"]);
    expect(astroBuildArgs("abc", "")).toEqual(["build", "--force"]);
  });
});

describe("collectRenderInputs", () => {
  const repo = join(dirname(fileURLToPath(import.meta.url)), "..", "..");
  const names = collectRenderInputs(repo).map((i) => i.name);

  it("reads every copied manifest, each walkthrough's file list, and the embed's plugin sources", () => {
    for (const id of ["sample-simulation", "set-up-monitoring"]) {
      expect(names).toContain(`docs/public/walkthroughs/${id}/walkthrough.json`);
      expect(names).toContain(`docs/public/walkthroughs/${id}/`);
    }
    expect(names).toEqual(
      expect.arrayContaining([
        "docs/src/plugins/remark-walkthroughs.mjs",
        "docs/src/plugins/walkthrough-embed.mjs",
        "docs/src/plugins/frontmatter-directive.mjs",
        "web/e2e/walkthroughcore.mjs",
      ]),
    );
  });

  it("lists a walkthrough's files, so a video added or removed changes the digest", () => {
    const listing = collectRenderInputs(repo).find((i) => i.name === "docs/public/walkthroughs/sample-simulation/");
    expect(String(listing?.bytes).split("\n")).toContain("walkthrough.json");
  });
});
