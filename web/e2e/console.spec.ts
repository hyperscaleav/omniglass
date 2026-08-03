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

// Home, the situation room. This is the one place the view path runs end to end
// against the real binary: the browser's query serialization, the server's
// param binding, the scoped query, and the renderers. The unit tests stub fetch,
// so a serialization mismatch between the generated client and the route's
// declared explode form would pass every one of them and fail only here.
test.describe("home, the situation room", () => {
  test.skip(!USER || !PASSWORD, "set OG_E2E_USER/OG_E2E_PASSWORD (run via `make test-e2e`)");

  test.beforeEach(async ({ page }) => {
    await page.goto("/web/login");
    await page.locator("#login-username").fill(USER as string);
    await page.locator("#login-password").fill(PASSWORD as string);
    await page.getByRole("button", { name: /sign in/i }).click();
    await page.waitForURL((url) => !url.pathname.endsWith("/login"));
  });

  test("renders the estate through views, with real rows in every panel", async ({ page }) => {
    // Every read Home makes must be a view run; a bespoke resource read would
    // mean the page had drifted off the read side it is meant to prove.
    const viewRuns: string[] = [];
    page.on("request", (req) => {
      const u = new URL(req.url());
      if (u.pathname.startsWith("/api/v1/views/")) viewRuns.push(u.pathname + u.search);
    });

    await page.goto("/web/");

    // The tiles come from estate-counts and carry the dev estate's real
    // numbers, so an empty or failed run is visible rather than a zero.
    const tiles = page.locator(".stat");
    await expect(tiles.first()).toBeVisible();
    await expect(page.getByText("components", { exact: true })).toBeVisible();
    const componentCount = await page.locator(".stat", { hasText: "components" }).locator(".stat-value").innerText();
    expect(Number(componentCount)).toBeGreaterThan(0);

    // The reachability grid carries a verdict per interface, readable as state
    // rather than only as colour.
    const states = page.locator("[data-state]");
    await expect(states.first()).toBeVisible();
    expect(await states.count()).toBeGreaterThan(0);

    // The feed carries occurrences from the seeded estate.
    await expect(page.getByRole("heading", { name: /recent occurrences/i })).toBeVisible();
    await expect(page.locator("table tbody tr").first()).toBeVisible();

    // No panel failed: a refused or broken view renders an alert.
    await expect(page.getByRole("alert")).toHaveCount(0);

    // The param binding survived the round trip. The feed asks for limit=8 as a
    // repeated param= pair, which is the form the route declares; a client that
    // serialized it any other way would have produced a 400 and an alert above.
    expect(viewRuns.some((r) => r.includes("event-feed:run"))).toBe(true);
    expect(viewRuns.some((r) => r.includes("param=limit%3D8"))).toBe(true);
    // The page honors the bound limit rather than the view's own default of 50.
    // Asserting the ceiling, not the fixture's row count: the seed's event count
    // is free to grow without falsifying the contract this checks.
    const feedRows = await page.locator("table tbody tr").count();
    expect(feedRows).toBeGreaterThan(0);
    expect(feedRows).toBeLessThanOrEqual(8);
  });

  test("streams a change into the page without a reload", async ({ page, request }) => {
    const token = process.env.OG_TOKEN;
    test.skip(!token, "set OG_TOKEN for the live-update leg");

    await page.goto("/web/");
    const componentTile = page.locator(".stat", { hasText: "components" }).locator(".stat-value");
    await expect(componentTile).toBeVisible();
    const before = Number(await componentTile.innerText());

    // Add a component through the API, the way an integration would, and watch
    // the tile move on its own: no reload, no click. This is the watch seam
    // proving itself against the real server.
    const name = `e2e-live-${Date.now()}`;
    const created = await request.post("/api/v1/components", {
      headers: { Authorization: `Bearer ${token}` },
      data: { name },
    });
    expect(created.ok()).toBeTruthy();

    try {
      await expect(componentTile).toHaveText(String(before + 1), { timeout: 20000 });
    } finally {
      // Leave the shared dev estate as we found it: a spec that accumulates a
      // component per run poisons the counts every later run asserts on.
      await request.delete(`/api/v1/components/${name}`, { headers: { Authorization: `Bearer ${token}` } });
    }
  });
});
