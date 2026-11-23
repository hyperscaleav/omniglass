import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { RouteGuard } from "./RouteGuard";
import { ME_KEY, type Me } from "../lib/auth";

afterEach(cleanup);

const viewer: Me = { user: { id: "v", kind: "person" }, person: { username: "viewer" }, permissions: ["*:read"], grants: [] };

function at(path: string, me: Me) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], me);
  window.history.pushState({}, "", `/web${path}`);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router base="/web">
        <Route path="/" component={() => <div>home page</div>} />
        <Route path="*" component={() => <RouteGuard><div>real page</div></RouteGuard>} />
      </Router>
    </QueryClientProvider>
  ));
}

// A route the caller may not read says so (#2442) rather than bouncing to Home.
describe("RouteGuard", () => {
  it.each([
    ["/settings", "Settings", "settings:read"],
    ["/secrets", "Secrets", "secret:read"],
    ["/users", "Users", "user:read:admin"],
  ])("renders the denied face for %s, not Home", async (path, section, perm) => {
    at(path, viewer);
    expect(await screen.findByText(`You do not have access to ${section}`)).toBeTruthy();
    expect(screen.getByTestId("denied-needs").textContent).toContain(perm);
    expect(screen.queryByText("real page")).toBeNull();
    expect(screen.queryByText("home page")).toBeNull();
    expect(window.location.pathname).toBe(`/web${path}`);
  });

  it("renders the page for a person who holds the permission", async () => {
    at("/settings", { ...viewer, permissions: ["settings:read"] });
    expect(await screen.findByText("real page")).toBeTruthy();
  });
});
