import { test, expect, type Page } from "@playwright/test";

// #2486: a hover explanation is reachable from the keyboard. Tabbing to an
// icon-only control shows its tooltip, which the native title attribute never
// did. Drives the alarms page, a component page and a catalog drawer.
const USER = process.env.OG_E2E_USER;
const PASSWORD = process.env.OG_E2E_PASSWORD;

// focusFirst puts keyboard focus on the first matching control (after a Tab, so the
// focus is keyboard-modal) and waits for its tooltip.
async function focusFirst(page: Page, selector: string) {
  const el = page.locator(selector).first();
  await el.waitFor();
  await page.keyboard.press("Tab");
  await el.focus();
  // The tip shown is the focused control's own: the trigger names it in aria-describedby.
  await expect(el).toHaveAttribute("aria-describedby", /.+/);
  const id = (await el.getAttribute("aria-describedby")) as string;
  const tip = page.locator(`[id="${id}"]`);
  await expect(tip).toBeVisible();
  await expect(tip).toHaveAttribute("role", "tooltip");
  await expect(tip).not.toHaveText("");
}

test.describe("keyboard tooltips", () => {
  test.skip(!USER || !PASSWORD, "set OG_E2E_USER/OG_E2E_PASSWORD (run via `make test-e2e`)");

  test.beforeEach(async ({ page }) => {
    await page.goto("/web/login");
    await page.locator("#login-username").fill(USER as string);
    await page.locator("#login-password").fill(PASSWORD as string);
    await page.getByRole("button", { name: /sign in/i }).click();
    await page.waitForURL((url) => !url.pathname.endsWith("/login"));
  });

  // The page's own icon-only controls and tips; an empty page falls back to the bar's.
  const icon = "main button.btn-square, main [tabindex='0']:not(tr), header button[aria-label='Ask omnibot']";

  test("tabbing to an icon-only control on the alarms page shows its tooltip", async ({ page }) => {
    await page.goto("/web/alarms");
    await focusFirst(page, icon);
  });

  test("tabbing to a control on a component's page shows its tooltip", async ({ page }) => {
    // A component whose name is unique addresses its page by name.
    const res = await page.request.get("/api/v1/components?limit=200");
    const names = ((await res.json()).components as { name: string }[]).map((c) => c.name);
    const name = names.find((n) => names.filter((x) => x === n).length === 1);
    test.skip(!name, "no component with a unique name");
    await page.goto(`/web/components/${name}`);
    await focusFirst(page, "main button.btn-square, main [tabindex='0']:not(tr)");
  });

  test("tabbing to a control in a catalog drawer shows its tooltip", async ({ page }) => {
    await page.goto("/web/metrics");
    await page.locator("main tbody tr, main [role=row]").nth(1).click();
    await expect(page.locator("aside.fixed")).toBeVisible();
    await focusFirst(page, "aside.fixed button");
  });
});
