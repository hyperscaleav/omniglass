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
    // What the LIST will show for it. A shipped fleet renders every location's
    // label from its own name ({{title (words .Name)}}, ADR-0105), and a row
    // whose label is generated shows that label and no second line, so the raw
    // name is on the detail and the label is on the list. Derived from the name
    // here rather than hard-coded, so the two stay one fact.
    const label = name.split("-").map((w) => w.charAt(0).toUpperCase() + w.slice(1)).join(" ");
    await page.getByRole("button", { name: /new location/i }).click();
    await page.getByLabel("Location type").selectOption("campus");
    await page.getByLabel("Name", { exact: true }).fill(name);
    await page.getByRole("button", { name: /create location/i }).click();

    // Create hands off to the new location's own detail in edit mode, and the
    // handoff is the URL itself (#759): the route lands carrying ?edit=1. Cancel
    // drops it to the read-only face (where the identity is rendered) and strips
    // the param, so the URL stops requesting an edit the operator left.
    await page.waitForURL(/\/web\/locations\/[0-9a-f-]{36}\?edit=1/);
    await page.getByRole("button", { name: /^cancel$/i }).first().click();
    await expect(page.getByText(name, { exact: true }).first()).toBeVisible();
    await expect(page).not.toHaveURL(/edit=1/);

    // The edit face is deep-linkable: revisiting the same detail with ?edit=1
    // lands editing directly, no clicks involved.
    await page.goto(page.url().split("?")[0] + "?edit=1");
    await expect(page.getByRole("button", { name: /save changes/i })).toBeVisible();
    await page.getByRole("button", { name: /^cancel$/i }).first().click();
    await expect(page).not.toHaveURL(/edit=1/);

    // It appears as a new root row back on the list, under the label the rule
    // rendered from the name typed above.
    await page.goto("/web/locations");
    await expect(page.locator("main")).toContainText(label);

    // Confirm-delete it from its own detail. The detail's Delete carries its
    // word; the row's inline action is an icon-only button of the same
    // accessible name.
    page.on("dialog", (d) => d.accept());
    await page.getByText(label, { exact: true }).first().click();
    await expect(page.getByText(name, { exact: true }).first()).toBeVisible();
    await expect(page.locator('button:text-is("Delete")')).toBeVisible();
    await page.locator('button:text-is("Delete")').click();

    // It is gone from the list.
    await expect(page.locator("main")).not.toContainText(label);
  });

  // The acceptance of #688, #699 and #702, and the only tier that can witness
  // any of them.
  //
  // The console shows the name and the label the platform is about to write,
  // both drafted by the server against a row that does not exist yet; the
  // gateway then mints and stamps the real ones inside the create's own
  // transaction. Nothing below this tier can prove the pair agrees: a page test
  // asserts what the form rendered and a storage test asserts what the gateway
  // wrote, and each is blind to the other.
  //
  // Since #702 the comparison is EXACT on both fields. It used to substitute an
  // ordinal into the shown value, because the name carried the token "n" and
  // the label carried it too: the ordinal is read from the placement bucket
  // before either is rendered, so the form shows the name the row lands with,
  // digits and all, and posts that number back as the create's precondition.
  test("a component created with both fields locked lands with the name and the label the console showed", async ({ page }) => {
    await page.goto("/web/components/create");

    // Choose what it is. Generic Device is the classification floor's fallback
    // and ships in every install, so this needs no fixture of its own.
    await page.getByLabel("Product").selectOption({ label: "Generic Device" });

    // Both identity fields are LOCKED on what the platform will use, and a
    // locked field posts nothing but the precondition (#699, #702). Locked is
    // READONLY and not disabled (#657), which a real browser is the only tier
    // that can check properly: the field is not editable, and it is still
    // focusable, so the value the row is about to carry has a keyboard path.
    const nameField = page.getByLabel("Name", { exact: true });
    await expect(nameField).toHaveValue(/^[a-z0-9-]+-\d+$/);
    await expect(nameField).not.toBeEditable();
    await expect(nameField).toBeEnabled();
    await nameField.focus();
    await expect(nameField).toBeFocused();
    const drafted = (await nameField.inputValue()).trim();
    // The number is real, which is the whole of #702: the field used to read
    // "device-n" here and the row then landed "device-1".
    expect(drafted).not.toContain("-n");

    const labelField = page.getByLabel("Label", { exact: true });
    await expect(labelField).not.toBeEditable();
    await expect(labelField).toBeEnabled();
    await expect(labelField).not.toHaveValue("");
    const draftedLabel = (await labelField.inputValue()).trim();

    // The override action is an icon button in each field, always present and
    // never hover-only, and focusing a locked field does not claim its pen: both
    // fields are still locked on the platform's answer after the focus above.
    await expect(page.getByRole("button", { name: "Override the name" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Override the label" })).toBeVisible();
    await expect(nameField).toHaveValue(drafted);

    await page.getByRole("button", { name: /create component/i }).click();
    await page.waitForURL(/\/web\/components\/[0-9a-f-]{36}/);

    // Create hands off to the new component's detail in edit mode; Cancel drops
    // it to the read-only face, where the identity is rendered rather than typed.
    await page.getByRole("button", { name: /^cancel$/i }).first().click();

    // What the row actually got, compared with what the operator was shown, on
    // both fields and with nothing substituted into either. A drift between the
    // draft's read of the bucket and the allocator's mint fails here and nowhere
    // else.
    await expect(page.getByText(drafted, { exact: true }).first()).toBeVisible();
    await expect(page.locator("main")).toContainText(draftedLabel);

    // And the platform holds the pen on both, which is what makes them the
    // platform's to keep current through a later move or reclassify. Posting the
    // precondition is what would break this if it were ever read as a name.
    await expect(page.getByText("Generated", { exact: true }).first()).toBeVisible();

    // Clean up after the run.
    page.on("dialog", (d) => d.accept());
    await page.locator('button:text-is("Delete")').click();
    await page.waitForURL(/\/web\/components\/?$/);
  });

  // #690, and the only tier that can witness it: the defect is a LAYOUT, so it
  // needs a browser doing layout. A page test can assert what the table declares
  // and jsdom will not measure a column, which is exactly how a Name column
  // measuring zero pixels sat on main behind a green suite.
  //
  // The numbers this replaced were measured on the dev fleet at a 1280 viewport,
  // where the list card offers 973px: Components' Name column was 0px wide (890px
  // of declared columns plus 150px of actions, with nothing left), Systems' was
  // 0px (960 declared), and Locations' was 173px (650 declared), which is why one
  // of the three looked fine and the acceptance names all three.
  for (const width of [1280, 1366]) {
    test(`the Name column survives a ${width}px viewport on every inventory page`, async ({ page }) => {
      await page.setViewportSize({ width, height: 800 });
      for (const path of ["/web/components", "/web/systems", "/web/locations"]) {
        await page.goto(path);
        const name = page.locator("main table.og-rows thead th").first();
        await expect(name).toHaveText("Name");
        const box = await name.boundingBox();
        // 150 rather than the floor itself (191px as of #693, 260 before the
        // label pen's chip left the cell and freed a measured 69px): the
        // assertion is that the identifier column is READABLE, not that it
        // equals a constant a later slice may tune, and this test has now
        // survived one such tune without editing. Zero, which is what two of
        // these three measured before the floor existed, fails it by a mile.
        expect(box, `${path} at ${width}px has no Name column at all`).not.toBeNull();
        expect(box!.width, `${path} at ${width}px: Name is ${Math.round(box!.width)}px`).toBeGreaterThan(150);
      }
    });
  }
});
