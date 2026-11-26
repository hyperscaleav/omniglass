import { test, expect } from "@playwright/test";

// An open drawer has an address (#2429), driven as an operator would on one flat
// list (event rules, a blade per row) and one tree list (files): a click writes
// ?open=, a reload and a pasted address land on the same drawer, Back closes it
// and stays on the list, and an address naming nothing shows the notice with no
// console error.
const USER = process.env.OG_E2E_USER;
const PASSWORD = process.env.OG_E2E_PASSWORD;

test.describe("drawer address", () => {
  test.skip(!USER || !PASSWORD, "set OG_E2E_USER/OG_E2E_PASSWORD (run via `make test-e2e`)");

  test.beforeEach(async ({ page }) => {
    await page.goto("/web/login");
    await page.locator("#login-username").fill(USER as string);
    await page.locator("#login-password").fill(PASSWORD as string);
    await page.getByRole("button", { name: "Sign in", exact: true }).click();
    await page.waitForURL((url) => !url.pathname.endsWith("/login"));
  });

  test("a flat list: open writes the address, reload and Back keep their word", async ({ page }) => {
    const errors: string[] = [];
    page.on("console", (m) => m.type() === "error" && !m.text().startsWith("Failed to load resource") && errors.push(m.text()));

    await page.goto("/web/event-rules?sort=name");
    await page.getByRole("row", { name: /Disk nearly full/ }).click();
    await expect(page).toHaveURL(/\/web\/event-rules\?.*open=disk-nearly-full/);
    await expect(page).toHaveURL(/sort=name/);
    const blade = page.locator("aside[data-blade]", { hasText: "Disk nearly full" });
    await expect(blade).toBeVisible();

    await page.reload();
    await expect(blade).toBeVisible();

    await page.goBack();
    await expect(blade).toBeHidden();
    await expect(page).toHaveURL(/\/web\/event-rules/);
    await expect(page).not.toHaveURL(/open=/);

    await page.goto("/web/event-rules?open=no-such-rule");
    await expect(page.getByTestId("drawer-miss")).toBeVisible();
    await expect(page.getByRole("row", { name: /Disk nearly full/ })).toBeVisible();
    expect(errors).toEqual([]);
  });

  test("a tree list: a file's drawer has an address by its id", async ({ page }) => {
    const name = `drawer-${Date.now()}.txt`;
    const made = await page.request.post("/api/v1/files", {
      data: { name, content_type: "text/plain", content: Buffer.from("hello").toString("base64"), sensitive: false },
    });
    expect(made.ok()).toBeTruthy();
    const id = (await made.json()).id as string;

    await page.goto("/web/files");
    await page.getByText(name, { exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`/web/files\\?open=${id}`));
    const blade = page.locator("aside[data-blade]", { hasText: name });
    await expect(blade).toBeVisible();

    await page.goBack();
    await expect(blade).toBeHidden();
    await expect(page).toHaveURL(/\/web\/files$/);

    await page.goto(`/web/files?open=${id}`);
    await expect(blade).toBeVisible();

    await page.request.delete(`/api/v1/files/${id}`);
  });
});
