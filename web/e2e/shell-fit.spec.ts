import { test, expect, type Page } from "@playwright/test";

// The lab strip is a thin line across the top and the console fits the
// viewport beneath it (#2433): the document never scrolls, the sidebar's user
// chip stays on screen, and the strip stays put when a page region scrolls.
// /auth/status is stubbed to say lab, so the case holds on any server.
const USER = process.env.OG_E2E_USER;
const PASSWORD = process.env.OG_E2E_PASSWORD;

async function stubLab(page: Page) {
  await page.route("**/auth/status", async (route) => {
    const res = await route.fetch();
    const body = await res.json();
    await route.fulfill({ response: res, json: { ...body, lab: true } });
  });
}

async function signIn(page: Page) {
  await page.goto("/web/login");
  await page.locator("#login-username").fill(USER as string);
  await page.locator("#login-password").fill(PASSWORD as string);
  await page.getByRole("button", { name: /sign in/i }).click();
  await page.waitForURL((url) => !url.pathname.endsWith("/login"));
}

async function expectFits(page: Page) {
  const m = await page.evaluate(() => ({
    scroll: document.documentElement.scrollHeight,
    inner: window.innerHeight,
    banner: document.querySelector('[data-testid="lab-banner"]')?.getBoundingClientRect().toJSON(),
    chip: document.querySelector("aside [data-testid='user-chip']")?.getBoundingClientRect().toJSON(),
  }));
  expect(m.scroll).toBe(m.inner);
  expect(m.banner).toBeTruthy();
  expect(m.banner.top).toBe(0);
  expect(m.banner.height).toBeLessThanOrEqual(28);
  expect(m.chip).toBeTruthy();
  expect(m.chip.bottom).toBeLessThanOrEqual(m.inner);
}

test.describe("lab banner and the shell fit the viewport", () => {
  test.skip(!USER || !PASSWORD, "set OG_E2E_USER/OG_E2E_PASSWORD (run via `make test-e2e`)");

  for (const size of [{ width: 1320, height: 860 }, { width: 1024, height: 800 }]) {
    test(`${size.width}x${size.height}: strip, no document scroll, chip on screen`, async ({ page }) => {
      await page.setViewportSize(size);
      await stubLab(page);
      await signIn(page);
      await page.goto("/web/explore");
      await expect(page.getByTestId("lab-banner")).toBeVisible();
      await expectFits(page);
      // Scrolling the page region leaves the strip in place.
      await page.evaluate(() => document.getElementById("scroll-main")?.scrollTo(0, 5000));
      await expectFits(page);
    });

    test(`${size.width}x${size.height}: with the impersonation banner too`, async ({ page }) => {
      await page.setViewportSize(size);
      await stubLab(page);
      await signIn(page);
      await page.evaluate(() => localStorage.setItem("og-impersonating", JSON.stringify({ target: "someone", mode: "view_as" })));
      await page.goto("/web/explore");
      await expect(page.getByTestId("impersonation-banner")).toBeVisible();
      await expectFits(page);
      const strips = await page.evaluate(() => ["lab-banner", "impersonation-banner"].map((id) => document.querySelector(`[data-testid="${id}"]`)!.getBoundingClientRect().toJSON()));
      expect(strips[1].top).toBeGreaterThanOrEqual(strips[0].bottom);
      expect(strips[1].height).toBeLessThanOrEqual(28);
    });
  }

  test("the sign-in page carries the same strip", async ({ page }) => {
    await page.setViewportSize({ width: 1320, height: 860 });
    await stubLab(page);
    await page.goto("/web/login");
    const b = await page.getByTestId("lab-banner").boundingBox();
    expect(b!.y).toBe(0);
    expect(b!.height).toBeLessThanOrEqual(28);
    expect(await page.evaluate(() => document.documentElement.scrollHeight === window.innerHeight)).toBe(true);
  });
});
