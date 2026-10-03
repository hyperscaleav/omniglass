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

  test("signs in, finds the outline, creates a location, opens it, deletes it", async ({ page }) => {
    await page.goto("/web/locations");

    // The bare index address lands on the outline (#798, #826, #861).
    await page.waitForURL(/\/web\/explore$/);
    await expect(page.getByRole("heading", { name: "Explore" })).toBeVisible();

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
    await page.getByRole("button", { name: "New", exact: true }).click();
    await page.getByRole("button", { name: "Location", exact: true }).click();
    await page.getByLabel("Location type").selectOption("campus");
    await page.getByLabel("Name", { exact: true }).fill(name);
    await page.getByRole("button", { name: /create location/i }).click();

    // Create hands off to the new location's own workspace already editing
    // (#800): the route lands carrying ?edit=1, which the zoom answers with
    // its Configure tab in edit mode. Cancel leaves edit and strips the
    // param, so the URL stops requesting an edit the operator left.
    await page.waitForURL(/\/web\/locations\/[0-9a-f-]{36}\?edit=1/);
    await expect(page.getByRole("button", { name: /save changes/i })).toBeVisible();
    await page.getByRole("button", { name: /^cancel$/i }).first().click();
    await expect(page.getByRole("heading", { name: label })).toBeVisible();
    await expect(page).not.toHaveURL(/edit=1/);

    // The edit is deep-linkable: revisiting with ?edit=1 lands the Configure
    // tab editing directly, no clicks involved.
    await page.goto(page.url().split("?")[0] + "?edit=1");
    await expect(page.getByRole("button", { name: /save changes/i })).toBeVisible();
    await page.getByRole("button", { name: /^cancel$/i }).first().click();
    await expect(page).not.toHaveURL(/edit=1/);

    // It appears as a new top-level place in the outline, under the label the
    // rule rendered from the name typed above.
    await page.goto("/web/explore");
    const row = page.getByRole("treeitem").filter({ has: page.locator("[data-label]", { hasText: label }) });
    await expect(row).toBeVisible();

    // Confirm-delete it from its blade: a row opens the blade (#799), whose
    // footer carries Delete behind a confirm.
    page.on("dialog", (d) => d.accept());
    await row.click();
    await expect(page.locator("aside[data-blade]")).toBeVisible();
    await expect(page.locator('aside[data-blade] button:text-is("Delete")')).toBeVisible();
    await page.locator('aside[data-blade] button:text-is("Delete")').click();

    // It is gone from the outline.
    await expect(row).toHaveCount(0);
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
    await page.waitForURL(/\/web\/components\/[0-9a-f-]{36}\?edit=1/);

    // Create hands off to the new leaf's Configure tab already editing (#800).
    // The platform holds the label's pen: the pen field says so in words while
    // the slot is editing (the old face's "Generated" chip retired in #693).
    await expect(page.getByRole("button", { name: /save changes/i })).toBeVisible();
    await expect(page.getByText(/Rendered from a label rule|No label rule applies/).first()).toBeVisible();

    // Cancel drops to the read-only leaf, where the identity is rendered
    // rather than typed.
    await page.getByRole("button", { name: /^cancel$/i }).first().click();

    // What the row actually got, compared with what the operator was shown, on
    // both fields and with nothing substituted into either. A drift between the
    // draft's read of the bucket and the allocator's mint fails here and nowhere
    // else.
    await expect(page.getByText(drafted, { exact: true }).first()).toBeVisible();
    await expect(page.locator("main")).toContainText(draftedLabel);

    // Clean up after the run, and prove on the way that a component placed
    // nowhere is still findable: it sits in the outline's "Placed nowhere you
    // can see" node, whose row blade carries the confirm-delete (#799).
    page.on("dialog", (d) => d.accept());
    await page.goto("/web/explore");
    await page.getByRole("button", { name: "Expand Placed nowhere you can see" }).click();
    const row = page.getByRole("treeitem").filter({ has: page.locator("[data-label]", { hasText: draftedLabel }) });
    await row.first().click();
    await expect(page.locator("aside[data-blade]")).toBeVisible();
    await page.locator('aside[data-blade] button:text-is("Delete")').click();
    await expect(row).toHaveCount(0);
  });

  // A LAYOUT, so only a browser can witness it (#690, #861). The outline's
  // Name column is the identifier an operator scans; on a narrow window the
  // other columns give way (Standard or product, then Type, then Detail) before it
  // does, and the page itself never scrolls sideways to make room. The
  // columns follow the outline's own width, which the sidebar shares.
  for (const width of [640, 900, 1280]) {
    test(`the outline keeps its Name column readable at ${width}px`, async ({ page }) => {
      // A place to measure, made through the API and removed after.
      const made = await page.request.post("/api/v1/locations", { data: { name: `e2e-squeeze-${width}-${Date.now()}`, location_type: "campus" } });
      expect(made.ok(), `create: ${made.status()}`).toBeTruthy();
      const { id } = (await made.json()) as { id: string };
      await page.setViewportSize({ width, height: 800 });
      await page.goto("/web/explore");
      const first = page.getByRole("treeitem").first();
      await expect(first).toBeVisible();
      // The Name cell is the row's first grid cell.
      const box = await first.locator(":scope > *").first().boundingBox();
      expect(box, `no Name cell at ${width}px`).not.toBeNull();
      expect(box!.width, `Name is ${Math.round(box!.width)}px at ${width}px`).toBeGreaterThanOrEqual(100);
      const tree = (await page.getByRole("tree").boundingBox())!;
      const health = (await first.getByTestId("health").boundingBox())!;
      expect(health.x + health.width, `Health runs past the card at ${width}px`).toBeLessThanOrEqual(tree.x + tree.width);
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      expect(overflow, `the page scrolls sideways at ${width}px`).toBeLessThanOrEqual(0);
      expect((await page.request.delete(`/api/v1/locations/${id}`)).ok()).toBeTruthy();
    });
  }

  test("explore: create where you stand, the outline shows it, a link reveals it, a row opens it", async ({ page }) => {
    // The e2e database starts with the boot seed only, so the places under
    // test are created here: a campus, two buildings under it from the row's
    // own +, and a system in the first.
    const stamp = Date.now();
    const campus = `e2e-fleet-${stamp}`;
    // A shipped fleet renders a location's label from its name
    // ({{title (words .Name)}}, ADR-0105).
    const titled = (n: string) => n.split("-").map((w) => w.charAt(0).toUpperCase() + w.slice(1)).join(" ");
    const campusLabel = titled(campus);
    const buildingLabel = titled(`${campus}-b`);
    const rowOf = (label: string) => page.getByRole("treeitem").filter({ has: page.locator("[data-label]", { hasText: new RegExp(`^${label}$`) }) });

    await page.goto("/web/locations/create");
    await page.getByLabel("Location type").selectOption("campus");
    await page.getByLabel("Name", { exact: true }).fill(campus);
    await page.getByRole("button", { name: /create location/i }).click();
    await page.waitForURL(/\/web\/locations\/[0-9a-f-]{36}/);
    const campusId = page.url().match(/locations\/([0-9a-f-]{36})/)![1];
    await page.getByRole("button", { name: /^cancel$/i }).first().click();

    // Create where you stand: the row's + places the new row under it. Once
    // the campus holds one building the two fold into one row ("Campus /
    // Building"), and its + still offers the campus as its own section, which
    // is how the second building lands beside the first.
    const buildingIds: string[] = [];
    for (const suffix of ["b", "c"]) {
      await page.goto("/web/explore");
      await page.getByRole("button", { name: new RegExp(`^Add under ${campusLabel}( / |$)`) }).click();
      await page.getByRole("group", { name: `Under ${campusLabel}`, exact: true }).getByRole("button", { name: "Location", exact: true }).click();
      await page.waitForURL(/\/web\/locations\/create\?under=/);
      await page.getByLabel("Location type").selectOption("building");
      await page.getByLabel("Name", { exact: true }).fill(`${campus}-${suffix}`);
      await page.getByRole("button", { name: /create location/i }).click();
      await page.waitForURL(/\/web\/locations\/[0-9a-f-]{36}\?edit=1/);
      buildingIds.push(page.url().match(/locations\/([0-9a-f-]{36})/)![1]);
    }
    const [buildingId, secondId] = buildingIds;

    // Prove the placement through the API before judging the render: both
    // buildings must actually sit under the campus, or the outline is being
    // asked the wrong question.
    for (const id of buildingIds) {
      const body = (await (await page.request.get(`/api/v1/locations/${id}`)).json()) as { parent_id?: string; location_type?: string };
      expect(body.parent_id, `location ${id} parent`).toBe(campusId);
      expect(body.location_type, `location ${id} type`).toBe("building");
    }

    // The system goes in the first building, from that row's +.
    await page.goto(`/web/explore?node=${buildingId}`);
    await rowOf(buildingLabel).getByRole("button", { name: `Add under ${buildingLabel}` }).click();
    await page.getByRole("button", { name: "System", exact: true }).click();
    await page.waitForURL(/\/web\/systems\/create\?under=/);
    await page.getByLabel("Name", { exact: true }).fill(`${campus}-sys`);
    await page.getByRole("button", { name: /create system/i }).click();
    await page.waitForURL(/\/web\/systems\/[0-9a-f-]{36}\?edit=1/);
    const systemId = page.url().match(/systems\/([0-9a-f-]{36})/)![1];

    // The outline: the campus says what it holds in the registry's words, and
    // expanding it shows both buildings. Expansion persists per browser by
    // design, and the reveal above opened the campus, so start collapsed.
    await page.evaluate(() => localStorage.removeItem("explore-open"));
    await page.goto("/web/explore");
    await expect(rowOf(campusLabel)).toContainText("2 buildings");
    await rowOf(campusLabel).getByRole("button", { name: `Expand ${campusLabel}` }).click();
    await expect(rowOf(buildingLabel)).toBeVisible();
    await expect(rowOf(titled(`${campus}-c`))).toBeVisible();

    // A name-shaped link reveals the row, expanded down to it and selected.
    await page.goto(`/web/explore?node=${campus}-b`);
    await expect(rowOf(buildingLabel)).toHaveAttribute("aria-selected", "true");

    // A place holding one system opens as that system.
    await rowOf(buildingLabel).click();
    await expect(page.locator("aside[data-blade]")).toHaveAttribute("aria-labelledby", `blade-title-system-${systemId}`);

    // The retired canvas address lands on the outline.
    await page.goto("/web/fleet");
    await page.waitForURL(/\/web\/explore$/);
    await expect(page.getByRole("tree", { name: "Places" })).toBeVisible();

    // Clean up: the system through its blade (the blade delete under test),
    // then the locations through the API on the browser's own session.
    page.on("dialog", (d) => d.accept());
    await page.goto(`/web/explore?node=${buildingId}`);
    await rowOf(buildingLabel).click();
    await page.locator('aside[data-blade] button:text-is("Delete")').click();
    await expect(page.locator("aside[data-blade]")).toHaveCount(0);
    await expect.poll(async () => (await page.request.get(`/api/v1/systems/${systemId}`)).status()).toBe(404);
    for (const id of [buildingId, secondId, campusId]) {
      const res = await page.request.delete(`/api/v1/locations/${id}`);
      expect(res.ok(), `delete location ${id}: ${res.status()}`).toBeTruthy();
    }
    await page.goto("/web/explore");
    await expect(rowOf(campusLabel)).toHaveCount(0);
  });
});
