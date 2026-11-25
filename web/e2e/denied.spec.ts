import { test, expect, type Page } from "@playwright/test";

// A page a person may not read says so (#2442). The lab's admin creates a
// read-only viewer (the shipped `viewer` role: *:read, which is not settings:read,
// secret:read or user:read:admin), signs in as it, and opens the pages by address:
// each shows the denied face, not Home and not a spinner, and Settings is absent
// from the sidebar. The server's refusal is the control; this asserts the console
// explains it.
const ADMIN = process.env.OG_E2E_USER;
const PASSWORD = process.env.OG_E2E_PASSWORD;

async function signIn(page: Page, username: string, password: string) {
  await page.goto("/web/login");
  await page.locator("#login-username").fill(username);
  await page.locator("#login-password").fill(password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.waitForURL((url) => !url.pathname.endsWith("/login"));
}

test.describe("denied pages", () => {
  test.skip(!ADMIN || !PASSWORD, "set OG_E2E_USER/OG_E2E_PASSWORD (run via `make test-e2e`)");

  const viewer = `e2e-viewer-${Date.now()}`;
  const viewerPassword = "Viewer-pass-2442-xyz";
  let userId = "";

  test.beforeAll(async ({ browser, baseURL }) => {
    const ctx = await browser.newContext({ baseURL });
    const page = await ctx.newPage();
    await signIn(page, ADMIN as string, PASSWORD as string);
    const made = await page.request.post("/api/v1/users", { data: { username: viewer, label: "E2E Viewer", password: viewerPassword } });
    expect(made.ok()).toBeTruthy();
    userId = ((await made.json()) as { id: string }).id;
    const grant = await page.request.post(`/api/v1/users/${userId}/grants`, { data: { role: "viewer", scope_kind: "all" } });
    expect(grant.ok()).toBeTruthy();
    await ctx.close();
  });

  test.afterAll(async ({ browser, baseURL }) => {
    if (!userId) return;
    const ctx = await browser.newContext({ baseURL });
    const page = await ctx.newPage();
    await signIn(page, ADMIN as string, PASSWORD as string);
    await page.request.post(`/api/v1/users/${userId}:archive`);
    await ctx.close();
  });

  test("Settings is absent from the sidebar and /settings shows the denied face", async ({ page }) => {
    await signIn(page, viewer, viewerPassword);
    await expect(page.getByRole("navigation").getByText("Settings", { exact: true })).toHaveCount(0);
    await page.goto("/web/settings");
    await expect(page.getByText("You do not have access to Settings")).toBeVisible();
    await expect(page.getByTestId("denied-needs")).toContainText("settings:read");
    await expect(page.locator(".loading-spinner")).toHaveCount(0);
    expect(new URL(page.url()).pathname).toBe("/web/settings");
  });

  for (const [path, section, perm] of [
    ["/web/secrets", "Secrets", "secret:read"],
    ["/web/users", "Users", "user:read:admin"],
  ]) {
    test(`${path} shows the denied face, not Home`, async ({ page }) => {
      await signIn(page, viewer, viewerPassword);
      await page.goto(path);
      await expect(page.getByText(`You do not have access to ${section}`)).toBeVisible();
      await expect(page.getByTestId("denied-needs")).toContainText(perm);
      expect(new URL(page.url()).pathname).toBe(path);
      await expect(page.getByRole("link", { name: "Profile" }).first()).toBeVisible();
    });
  }
});
