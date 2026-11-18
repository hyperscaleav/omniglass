import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

// Platform-authored examples (console placeholders, code comments) must use the
// fictional catalog (boreal, kestrel, lyra, newtron) rather than real manufacturer
// names (crestron, cisco, samsung). This test ensures no real names leak into the
// source files that render operator-facing surfaces or API documentation.

const here = dirname(fileURLToPath(import.meta.url));

const forbidden = [
  /\bcrestron\b/gi,
  /\bCrestron\b/,
  /\bCisco codec\b/,
  /\bcisco-room-bar\b/gi,
  /\bSamsung QM55\b/,
];

describe("Catalog examples use fictional names", () => {
  it("Vendors.tsx has no real manufacturer names", () => {
    const content = readFileSync(join(here, "pages/Vendors.tsx"), "utf-8");
    for (const pattern of forbidden) {
      expect(content).not.toMatch(pattern);
    }
  });

  it("Products.tsx has no real manufacturer names", () => {
    const content = readFileSync(join(here, "pages/Products.tsx"), "utf-8");
    for (const pattern of forbidden) {
      expect(content).not.toMatch(pattern);
    }
  });

  it("lib/products.ts has no real manufacturer names in comments", () => {
    const content = readFileSync(join(here, "lib/products.ts"), "utf-8");
    for (const pattern of forbidden) {
      expect(content).not.toMatch(pattern);
    }
  });
});
