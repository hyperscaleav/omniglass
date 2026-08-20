import { test, expect } from "@playwright/test";
import { ROUTE_MANIFEST } from "../src/lib/routemanifest";

// The route smoke tier (#760): one test per manifest entry, visiting its smoke
// address and failing on any page error or console error. The manifest is the
// same array the router renders from (index.tsx), so a route cannot exist
// without this tier rendering it; a parametrized route smokes its miss face
// through the well-formed-unknown uuid, which is a render worth having.
//
// Console noise policy (no silent caps): a resource-load failure line
// ("Failed to load resource ...") is NOT collected, because an empty fleet
// legitimately 404s optional fetches (an avatar, an empty registry); every
// filtered line is printed so a sweep of the output still sees it. Everything
// else that reaches the console as an error, and every uncaught page error,
// fails the route's test.
const USER = process.env.OG_E2E_USER;
const PASSWORD = process.env.OG_E2E_PASSWORD;

test.describe("console routes", () => {
  test.skip(!USER || !PASSWORD, "set OG_E2E_USER/OG_E2E_PASSWORD (run via `make test-e2e`)");

  for (const r of ROUTE_MANIFEST) {
    test(`renders ${r.shell}:${r.path} at ${r.smoke}`, async ({ page }) => {
      const errors: string[] = [];
      page.on("pageerror", (e) => errors.push(`pageerror: ${e.message}`));
      page.on("console", (m) => {
        if (m.type() !== "error") return;
        if (m.text().startsWith("Failed to load resource")) {
          console.log(`[filtered resource error] ${r.smoke}: ${m.text()}`);
          return;
        }
        errors.push(`console.error: ${m.text()}`);
      });

      if (r.auth !== false) {
        await page.goto("/web/login");
        await page.locator("#login-username").fill(USER as string);
        await page.locator("#login-password").fill(PASSWORD as string);
        await page.getByRole("button", { name: /sign in/i }).click();
        await page.waitForURL((url) => !url.pathname.endsWith("/login"));
      }

      await page.goto("/web" + r.smoke, { waitUntil: "networkidle" });
      // The page rendered SOMETHING (the shell, a list, a stub, a miss face);
      // a blank root means the route resolved no component.
      await expect(page.locator("#root > *").first()).toBeVisible();
      expect(errors).toEqual([]);
    });
  }
});
