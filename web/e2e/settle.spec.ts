import { test, expect } from "@playwright/test";
import { isStreamRequest, settle, settleLayout, trackRequests } from "./settle.mjs";

// A live page holds its views stream open (#1830): settle waits on every
// request but that one.
test.describe("isStreamRequest", () => {
  test("an event stream, by its Accept or its route, is a stream", () => {
    expect(isStreamRequest("http://h/api/v1/anything", "text/event-stream")).toBe(true);
    expect(isStreamRequest("http://h/api/v1/views:watch?bindings=%5B%5D", undefined)).toBe(true);
  });
  test("a read that ends is not", () => {
    expect(isStreamRequest("http://h/api/v1/views:run", "application/json")).toBe(false);
    expect(isStreamRequest("http://h/api/v1/platform", undefined)).toBe(false);
    expect(isStreamRequest("not a url", undefined)).toBe(false);
  });
});

test("settle waits out a read but not an open stream", async ({ page }) => {
  await page.route("**/slow", async (route) => {
    await new Promise((r) => setTimeout(r, 400));
    await route.fulfill({ body: "done" });
  });
  await page.route("**/api/v1/views:watch*", () => {
    // Never answered: the stream a live page holds open.
  });
  await page.route("http://settle.test/", (route) => route.fulfill({ contentType: "text/html", body: "<p>page</p>" }));
  trackRequests(page);
  await page.goto("http://settle.test/");
  const started = Date.now();
  await page.evaluate(() => {
    void fetch("/api/v1/views:watch?bindings=%5B%5D", { headers: { Accept: "text/event-stream" } });
    void fetch("/slow");
  });
  await settle(page, { quietMs: 200, timeout: 5_000 });
  expect(Date.now() - started).toBeGreaterThanOrEqual(400);
});

test("settleLayout waits for a scroll in flight and then returns", async ({ page }) => {
  await page.route("http://settle.test/", (route) =>
    route.fulfill({
      contentType: "text/html",
      body: '<div id=s style="width:100px;overflow:auto"><div style="width:2000px">x</div></div>',
    }),
  );
  await page.goto("http://settle.test/");
  await page.evaluate(() => {
    const el = document.getElementById("s")!;
    let n = 0;
    const step = () => {
      el.scrollLeft = (n += 20);
      if (n < 400) setTimeout(step, 20);
    };
    step();
  });
  await settleLayout(page, { frames: 10, timeout: 5_000 });
  expect(await page.evaluate(() => document.getElementById("s")!.scrollLeft)).toBe(400);
});

test("settleLayout rejects a layout that never holds still", async ({ page }) => {
  await page.route("http://settle.test/", (route) => route.fulfill({ contentType: "text/html", body: '<div id=s style="overflow:auto;width:50px"><div style="width:999px">x</div></div>' }));
  await page.goto("http://settle.test/");
  await page.evaluate(() => {
    let n = 0;
    const loop = () => {
      document.getElementById("s")!.scrollLeft = ++n % 500;
      requestAnimationFrame(loop);
    };
    loop();
  });
  await expect(settleLayout(page, { frames: 10, timeout: 500 })).rejects.toThrow(/still moving/);
});
