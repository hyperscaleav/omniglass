import { test, expect } from "@playwright/test";

// The browser-driven e2e tier for the console: drive it as an operator would, end
// to end, against the real binary (the API, the typed client, the SPA), asserting
// the user-observable outcome. A full inventory CRUD round-trip exercises the
// shell, the typed client, the create/edit Drawer, the blade, and delete.
const USER = process.env.OG_E2E_USER;
const PASSWORD = process.env.OG_E2E_PASSWORD;

test.describe("operator console", () => {
  test.skip(!USER || !PASSWORD, "set OG_E2E_USER/OG_E2E_PASSWORD (run via `make test-e2e`)");

  test.beforeEach(async ({ page }) => {
    // Sign in through the real login form; the server sets the session cookie.
    await page.goto("/web/login");
    await page.locator("#login-username").fill(USER as string);
    await page.locator("#login-password").fill(PASSWORD as string);
    await page.getByRole("button", { name: /sign in/i }).click();
    await page.waitForURL((url) => !url.pathname.endsWith("/login"));
  });

  test("signs in, lists locations, creates a location, opens it, deletes it", async ({ page }) => {
    await page.goto("/web/locations");

    // The shell labels the section and the inventory surface renders.
    await expect(page.getByRole("banner")).toContainText(/locations/i);

    // Create a throwaway campus through the form Drawer.
    const name = `e2e-${Date.now()}`;
    await page.getByRole("button", { name: /new location/i }).click();
    await page.getByPlaceholder("hq-a-301").fill(name);
    await page.locator('input[list="loc-types"]').fill("campus");
    await page.getByRole("button", { name: /create location/i }).click();

    // It appears as a new root row.
    await expect(page.locator("main")).toContainText(name);

    // Open it as a blade and confirm-delete it.
    page.on("dialog", (d) => d.accept());
    await page.getByText(name, { exact: true }).first().click();
    const blade = page.getByRole("dialog");
    await expect(blade).toContainText(name);
    await blade.getByRole("button", { name: /^delete$/i }).click();

    // It is gone from the list.
    await expect(page.locator("main")).not.toContainText(name);
  });
});
