import { test, expect } from "@playwright/test";

// The command palette finds things, not just pages (#2444): from the keyboard
// alone, type a system's name and land on its page. The system is made through
// the API (the lab is the empty scenario), then found by typing, opened with
// Enter, and a second search proves the first use left a recent item behind.
const USER = process.env.OG_E2E_USER;
const PASSWORD = process.env.OG_E2E_PASSWORD;

test.describe("palette find", () => {
  test.skip(!USER || !PASSWORD, "set OG_E2E_USER/OG_E2E_PASSWORD (run via `make test-e2e`)");

  test.beforeEach(async ({ page }) => {
    await page.goto("/web/login");
    await page.locator("#login-username").fill(USER as string);
    await page.locator("#login-password").fill(PASSWORD as string);
    await page.getByRole("button", { name: /sign in/i }).click();
    await page.waitForURL((url) => !url.pathname.endsWith("/login"));
  });

  test("a seeded system is found by name and Enter opens its page", async ({ page }) => {
    const stamp = Date.now();
    const loc = `e2e-hall-${stamp}`;
    const system = `e2e-boreal-${stamp}`;
    expect((await page.request.post("/api/v1/locations", { data: { name: loc, location_type: "campus" } })).ok()).toBeTruthy();
    const made = await page.request.post("/api/v1/systems", { data: { name: system, label: "Boreal Rack", location: loc } });
    expect(made.ok()).toBeTruthy();
    const id = (await made.json()).id as string;

    await page.goto("/web/");
    await page.keyboard.press("Control+k");
    const field = page.getByRole("combobox", { name: "Search the console" });
    await expect(field).toBeFocused();
    await page.keyboard.type(system);

    const row = page.getByRole("option", { name: new RegExp(system) });
    await expect(row).toBeVisible();
    await expect(row).toContainText(loc);
    // The first match is the default; Ask omnibot, last, is never it.
    await expect(page.locator("[role=option][data-first]")).toContainText(system);

    await page.keyboard.press("Enter");
    await expect(page).toHaveURL(new RegExp(`/web/systems/${id}`));
    await expect(page.getByRole("dialog", { name: "Command palette" })).toBeHidden();

    // Reopened with nothing typed, the system is the first recent item.
    await page.keyboard.press("Control+k");
    await expect(page.locator("[data-group-start='Recent']")).toContainText(system);
  });

  test("Escape closes the palette and the focus returns to where it was", async ({ page }) => {
    await page.goto("/web/");
    const opener = page.getByRole("button", { name: /^Search/ }).first();
    await opener.focus();
    await page.keyboard.press("Control+k");
    await expect(page.getByRole("combobox", { name: "Search the console" })).toBeFocused();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("dialog", { name: "Command palette" })).toBeHidden();
    await expect(opener).toBeFocused();
  });
});
