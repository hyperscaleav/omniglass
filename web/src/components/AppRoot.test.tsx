import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";

vi.mock("../api/client", () => ({
  api: { GET: vi.fn(async () => ({ data: { bootstrapped: true, lab: true } })) },
}));

import AppRoot from "./AppRoot";

afterEach(cleanup);

// The lab banner is the topmost element on every screen (#2275): above the
// login card and above a signed-in shell's top bar alike.
describe("AppRoot", () => {
  const mount = (screen: () => any) =>
    render(() => (
      <QueryClientProvider client={new QueryClient()}>
        <AppRoot>{screen()}</AppRoot>
      </QueryClientProvider>
    ));

  it.each([
    ["login", () => <div data-testid="screen">login card</div>],
    ["a signed-in route", () => <header data-testid="screen">top bar</header>],
  ])("puts the banner above %s", async (_n, screen) => {
    const { findByTestId, getByTestId } = mount(screen);
    const banner = await findByTestId("lab-banner");
    const first = banner.closest("[data-app-root]")!.firstElementChild;
    expect(first).toBe(banner);
    expect(banner.compareDocumentPosition(getByTestId("screen")) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("renders the banner once", async () => {
    const { findAllByTestId } = mount(() => <div />);
    await waitFor(async () => expect((await findAllByTestId("lab-banner")).length).toBe(1));
  });
});
