import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import DeniedPage from "./DeniedPage";

afterEach(cleanup);

const mount = (el: () => any) => {
  window.history.pushState({}, "", "/web/x");
  return render(() => (
    <Router base="/web">
      <Route path="*" component={el} />
    </Router>
  ));
};

// The denied face (#2442): a whole page the person may not read says so, names the
// permission (the console teaches the model), and offers the way back. It carries
// nothing about what exists behind the page.
describe("DeniedPage", () => {
  it("names the section and the permission it needs", async () => {
    mount(() => <DeniedPage section="Settings" needs={["settings:read"]} />);
    expect(await screen.findByText("You do not have access to Settings")).toBeTruthy();
    expect(screen.getByTestId("denied-needs").textContent).toContain("settings:read");
    expect(screen.getByRole("alert")).toBeTruthy();
  });

  it("lists alternatives when any one of several permissions opens the page", async () => {
    mount(() => <DeniedPage section="Explore" needs={["location:read", "system:read", "component:read"]} />);
    const needs = (await screen.findByTestId("denied-needs")).textContent ?? "";
    expect(needs).toContain("location:read");
    expect(needs).toContain("component:read");
    expect(needs).toMatch(/any of/i);
  });

  it("links back to Home and to Profile, where the person's access is listed", async () => {
    mount(() => <DeniedPage section="Users" needs={["user:read:admin"]} />);
    const home = (await screen.findByRole("link", { name: /home/i })) as HTMLAnchorElement;
    const profile = screen.getByRole("link", { name: /profile/i }) as HTMLAnchorElement;
    expect(home.getAttribute("href")).toMatch(/^\/web\/?$/);
    expect(profile.getAttribute("href")).toBe("/web/profile");
  });

  it("omits the permission line when none is known and still renders", async () => {
    mount(() => <DeniedPage section="This page" />);
    expect(await screen.findByText("You do not have access to This page")).toBeTruthy();
    expect(screen.queryByTestId("denied-needs")).toBeNull();
  });
});
