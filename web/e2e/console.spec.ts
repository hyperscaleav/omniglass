import { test, expect } from "@playwright/test";

// The browser-driven e2e tier for the console: drive it as an operator would, end
// to end, against the real binary (the API, the typed client, the SPA), asserting
// the user-observable outcome. A full inventory CRUD round-trip exercises the
// shell, the typed client, the create-as-route draft, the detail, and delete.
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

    // Create a throwaway campus through the create-as-route draft. Campus
    // carries no name rule, so the operator types the name: the other half of
    // the same form is proven by the component case below.
    const name = `e2e-${Date.now()}`;
    await page.getByRole("button", { name: /new location/i }).click();
    await page.getByLabel("Location type").selectOption("campus");
    await page.getByLabel("Name", { exact: true }).fill(name);
    await page.getByRole("button", { name: /create location/i }).click();

    // Create hands off to the new location's own detail in edit mode; Cancel
    // drops it to the read-only face, which is where the identity is rendered.
    await page.waitForURL(/\/web\/locations\/[0-9a-f-]{36}/);
    await page.getByRole("button", { name: /^cancel$/i }).first().click();
    await expect(page.getByText(name, { exact: true }).first()).toBeVisible();

    // It appears as a new root row back on the list.
    await page.goto("/web/locations");
    await expect(page.locator("main")).toContainText(name);

    // Confirm-delete it from its own detail. The detail's Delete carries its
    // word; the row's inline action is an icon-only button of the same
    // accessible name.
    page.on("dialog", (d) => d.accept());
    await page.getByText(name, { exact: true }).first().click();
    await expect(page.locator('button:text-is("Delete")')).toBeVisible();
    await page.locator('button:text-is("Delete")').click();

    // It is gone from the list.
    await expect(page.locator("main")).not.toContainText(name);
  });

  // The acceptance of #688 and #699, and the only tier that can witness either.
  //
  // The console shows the SHAPE of the name a nameless create will be given,
  // resolved from the chosen product's component_type chain in the browser; the
  // gateway mints the actual name from its own walk of the same chain, inside
  // the create's transaction. And the console shows the LABEL the platform will
  // write, rendered by the server against a row that does not exist yet; the
  // gateway stamps the real one inside that same transaction. Nothing below
  // this tier can prove either pair agrees: a page test asserts what the form
  // rendered and a storage test asserts what the gateway wrote, and each is
  // blind to the other.
  test("a component created with both fields locked lands with the name and the label the console showed", async ({ page }) => {
    await page.goto("/web/components/create");

    // Choose what it is. Generic Device is the classification floor's fallback
    // and ships in every install, so this needs no fixture of its own.
    await page.getByLabel("Product").selectOption({ label: "Generic Device" });

    // Both identity fields are LOCKED on what the platform will use, and a
    // locked field posts nothing (#699). The name's ordinal is written as a
    // token because it does not exist yet. Locked is READONLY and not disabled
    // (#657), which a real browser is the only tier that can check properly: the
    // field is not editable, and it is still focusable, so the value the row is
    // about to carry has a keyboard path.
    const nameField = page.getByLabel("Name", { exact: true });
    await expect(nameField).not.toBeEditable();
    await expect(nameField).toBeEnabled();
    await nameField.focus();
    await expect(nameField).toBeFocused();
    const shape = (await nameField.inputValue()).trim();
    expect(shape).toMatch(/^[a-z0-9-]+-n$/);
    const stem = shape.slice(0, -2);

    const labelField = page.getByLabel("Display name", { exact: true });
    await expect(labelField).not.toBeEditable();
    await expect(labelField).toBeEnabled();
    await expect(labelField).not.toHaveValue("");
    const drafted = (await labelField.inputValue()).trim();

    // The override action is an icon button in each field, always present and
    // never hover-only, and focusing a locked field does not claim its pen: both
    // fields are still locked on the platform's answer after the focus above.
    await expect(page.getByRole("button", { name: "Override the name" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Override the display name" })).toBeVisible();
    await expect(nameField).toHaveValue(shape);

    await page.getByRole("button", { name: /create component/i }).click();
    await page.waitForURL(/\/web\/components\/[0-9a-f-]{36}/);

    // Create hands off to the new component's detail in edit mode; Cancel drops
    // it to the read-only face, where the identity is rendered rather than typed.
    await page.getByRole("button", { name: /^cancel$/i }).first().click();

    // What the row actually got: the stem the console resolved in the browser,
    // and an ordinal the console could not have known, minted by the gateway's
    // own walk of the same chain inside the create's transaction. A disagreement
    // between the two walks fails here and nowhere else.
    const minted = page.getByText(new RegExp(`^${stem}-\\d+$`)).first();
    await expect(minted).toBeVisible();
    const mintedName = ((await minted.textContent()) ?? "").trim();
    const ordinal = mintedName.slice(stem.length + 1);

    // And the LABEL the row carries is the one the form showed, with the ordinal
    // filled into the one place the render could not know it. The token is
    // substituted only where it stands alone as a word, so a type name carrying
    // the letter is left intact.
    const expected = drafted.replace(/\bn\b/, ordinal);
    expect(expected).not.toBe(drafted);
    await expect(page.locator("main")).toContainText(expected);

    // And the platform holds the pen on both, which is what makes them the
    // platform's to keep current through a later move or reclassify.
    await expect(page.getByText("Generated", { exact: true }).first()).toBeVisible();

    // Clean up after the run.
    page.on("dialog", (d) => d.accept());
    await page.locator('button:text-is("Delete")').click();
    await page.waitForURL(/\/web\/components\/?$/);
  });
});
