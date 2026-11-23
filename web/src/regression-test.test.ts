import { describe, it, expect } from "vitest";
import { check, closedIssues, exemption, isTestFile, touchesTest } from "../e2e/regressiontest.mjs";

describe("closedIssues", () => {
  it("reads closing keyword variants, once each", () => {
    expect(closedIssues("Closes #1\nfixed #2, Resolves: #3\nfixes #1\ncloses org/repo#4")).toEqual([1, 2, 3, 4]);
  });
  it("ignores a bare reference, a comment and a fence", () => {
    expect(closedIssues("see #5\n<!-- closes #6 -->\n```\ncloses #7\n```\nrefs #8")).toEqual([]);
  });
});

describe("exemption", () => {
  it("returns the reason", () => expect(exemption("x\nregression-test: none (needs real hardware)\n")).toBe("needs real hardware"));
  it("returns an empty reason for empty or missing parentheses", () => {
    expect(exemption("regression-test: none ()")).toBe("");
    expect(exemption("regression-test: none")).toBe("");
  });
  it("is null with no line, in a comment, or in a fence", () => {
    expect(exemption("nothing")).toBeNull();
    expect(exemption("<!-- regression-test: none (x) -->")).toBeNull();
    expect(exemption("```\nregression-test: none (x)\n```")).toBeNull();
  });
});

describe("isTestFile", () => {
  it.each(["internal/a/a_test.go", "web/src/a.test.ts", "web/src/a.test.tsx", "web/src/a.spec.ts", "web/e2e/walkthroughs/x.yaml"])("%s is a test", (p) =>
    expect(isTestFile(p)).toBe(true));
  it.each(["internal/a/a.go", "web/src/a.tsx", "docs/x.md", "internal/a/testdata/x.json"])("%s is not", (p) => expect(isTestFile(p)).toBe(false));
  it("testdata counts only beside a test", () => {
    expect(touchesTest(["internal/a/testdata/x.json"])).toBe(false);
    expect(touchesTest(["internal/a/testdata/x.json", "internal/a/a_test.go"])).toBe(true);
  });
});

describe("check", () => {
  const bug = [{ number: 9, type: "Bug" }];
  it("fails a bug close with no test, naming the issue and both ways out", () => {
    const v = check({ body: "Closes #9", changed: ["a.go"], closed: bug });
    expect(v.ok).toBe(false);
    expect(v.message).toContain("#9");
    expect(v.message).toContain("regression-test: none (");
  });
  it("passes with a test change", () => expect(check({ body: "Closes #9", changed: ["a.go", "a_test.go"], closed: bug }).ok).toBe(true));
  it("passes with a reasoned exemption", () =>
    expect(check({ body: "Closes #9\nregression-test: none (docs typo)", changed: ["a.go"], closed: bug }).ok).toBe(true));
  it("fails an empty reason", () => {
    const v = check({ body: "Closes #9\nregression-test: none ()", changed: ["a.go"], closed: bug });
    expect(v.ok).toBe(false);
    expect(v.message).toContain("no reason");
  });
  it("gates only the Bug type", () => expect(check({ body: "", changed: [], closed: [{ number: 3, type: "Task" }] }).ok).toBe(true));
  it("passes non-bug or no closes", () => {
    expect(check({ body: "", changed: ["a.go"], closed: [{ number: 1, type: "Feature" }] }).ok).toBe(true);
    expect(check({ body: "", changed: ["a.go"], closed: [] }).ok).toBe(true);
  });
});
