import { test, expect, type Page } from "@playwright/test";
import { LESSONS } from "../src/lib/lessons";
import { gotoSettled } from "./settle.mjs";

// The Learn page (#2450), driven as a learner would: open every lesson, check
// each step's live element rendered, and follow every "Do it in the console"
// link to a page that is not the not-found face and raised no page error. The
// lesson list is the same array the page renders, so a lesson cannot be added
// without this tier walking it. Run in both of run.sh's phases: with no
// simulator (the empty scenario) a lesson says so and its component links go
// to the lab instructions; with the dev fleet (@simulator) every live element
// renders over a simulated component.
const USER = process.env.OG_E2E_USER;
const PASSWORD = process.env.OG_E2E_PASSWORD;
const SIM = !!process.env.OG_E2E_SIMULATOR;

async function signIn(page: Page) {
  await page.goto("/web/login");
  await page.locator("#login-username").fill(USER as string);
  await page.locator("#login-password").fill(PASSWORD as string);
  await page.getByRole("button", { name: /sign in/i }).click();
  await page.waitForURL((url) => !url.pathname.endsWith("/login"));
}

function watchErrors(page: Page): string[] {
  const errors: string[] = [];
  page.on("pageerror", (e) => errors.push(`pageerror: ${e.message}`));
  return errors;
}

async function walkEveryLesson(page: Page) {
  const errors = watchErrors(page);
  await gotoSettled(page, "/web/learn");
  await expect(page.locator("[data-lesson]")).toHaveCount(LESSONS.length);
  if (!SIM) await expect(page.getByTestId("learn-no-simulator")).toBeVisible();

  for (const lesson of LESSONS) {
    await gotoSettled(page, `/web/learn/${lesson.slug}`);
    await expect(page.getByRole("heading", { level: 1 })).toContainText(lesson.title);
    const steps = page.getByTestId("lesson-step");
    await expect(steps).toHaveCount(lesson.steps.length);
    if (SIM) {
      for (const [i, step] of lesson.steps.entries()) {
        if (step.live) await expect(steps.nth(i).getByTestId(`lesson-live-${step.live}`), `${lesson.slug} step ${i + 1}`).toBeVisible({ timeout: 30_000 });
      }
    } else {
      await expect(page.locator('[data-testid^="lesson-live-"]')).toHaveCount(0);
    }
    const hrefs = await page.getByTestId("lesson-do-it").evaluateAll((els) => els.map((e) => e.getAttribute("href") ?? ""));
    for (const href of hrefs) {
      // The router renders each link under its /web base already.
      await gotoSettled(page, href.startsWith("/web/") ? href : "/web" + href);
      await expect(page.locator("#root > *").first()).toBeVisible();
      await expect(page.getByText("That page does not exist in the console."), `${lesson.slug} -> ${href}`).toHaveCount(0);
    }
  }
  expect(errors).toEqual([]);
}

test.describe("learn", () => {
  test.skip(!USER || !PASSWORD, "set OG_E2E_USER/OG_E2E_PASSWORD (run via `make test-e2e`)");
  test.beforeEach(async ({ page }) => signIn(page));

  test("opens every lesson and follows every console link, with no simulator", async ({ page }) => {
    test.skip(SIM, "the no-simulator face runs in run.sh's first phase");
    test.setTimeout(120_000);
    await walkEveryLesson(page);
  });

  test("opens every lesson over a simulated component and follows every console link @simulator", async ({ page }) => {
    test.skip(!SIM, "needs a lab with a simulator (run.sh's second phase)");
    test.setTimeout(180_000);
    await walkEveryLesson(page);
  });

  test("remembers a finished lesson for this person @simulator", async ({ page }) => {
    test.skip(!SIM, "needs a lab with a simulator (run.sh's second phase)");
    await gotoSettled(page, "/web/learn/device-to-samples");
    const done = page.getByRole("button", { name: "Mark as done" });
    const undo = page.getByRole("button", { name: "Mark as not done" });
    await expect(done.or(undo)).toBeVisible();
    if (await undo.isVisible()) await undo.click();
    await page.getByRole("button", { name: "Mark as done" }).click();
    await expect(page.getByTestId("lesson-done")).toBeVisible();
    await page.reload();
    await expect(page.getByTestId("lesson-done")).toBeVisible();
    await gotoSettled(page, "/web/learn");
    await expect(page.locator('[data-lesson="device-to-samples"] [data-testid="lesson-done"]')).toBeVisible();
    await gotoSettled(page, "/web/learn/device-to-samples");
    await page.getByRole("button", { name: "Mark as not done" }).click();
    await expect(page.getByRole("button", { name: "Mark as done" })).toBeVisible();
  });
});
