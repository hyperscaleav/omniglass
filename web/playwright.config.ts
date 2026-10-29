import { defineConfig } from "@playwright/test";

// Drives the console served by the real Go binary. Run via `make test-e2e`, which
// brings up a throwaway lab and exports OG_E2E_BASE, OG_TOKEN, OG_E2E_USER and
// OG_E2E_PASSWORD (lab_pw in web/e2e/lib/lab.sh). The specs skip when the login
// is absent, so a bare `vitest`/`playwright` run does not fail.
export default defineConfig({
  testDir: "./e2e",
  timeout: 30_000,
  expect: { timeout: 7_000 },
  reporter: "list",
  use: {
    baseURL: process.env.OG_E2E_BASE ?? "http://localhost:8080",
    headless: true,
  },
});
