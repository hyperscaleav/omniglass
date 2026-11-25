import { describe, it, expect } from "vitest";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

// A lab run with single sign-on (--sso) renders a "Sign in with SSO" button
// beside "Sign in", so a locator that matches /sign in/i resolves to both and
// Playwright's strict mode throws: the probe could not sign in and recorded
// nothing (#2589), and neither could the SSO walkthrough. The e2e code names
// the password button exactly.
const E2E = join(__dirname, "../../e2e");

function files(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((d) => {
    const p = join(dir, d.name);
    if (d.isDirectory()) return d.name === "node_modules" || d.name === "out" ? [] : files(p);
    return /\.(ts|mjs|js)$/.test(d.name) ? [p] : [];
  });
}

describe("the e2e sign-in locator", () => {
  it("never matches /sign in/i, which also matches the SSO button", () => {
    const hits: string[] = [];
    for (const f of files(E2E)) {
      readFileSync(f, "utf8")
        .split("\n")
        .forEach((line, i) => {
          if (/name:\s*\/sign in\/i/.test(line)) hits.push(`${f.slice(E2E.length + 1)}:${i + 1}`);
        });
    }
    expect(hits, "use { name: \"Sign in\", exact: true }").toEqual([]);
  });
});
