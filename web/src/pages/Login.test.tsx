import "@testing-library/jest-dom/vitest";
import { afterEach, describe, it, expect, vi } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Login from "./Login";

// The sign-in page offers single sign-on (#2362) when /auth/status names a
// provider, and shows the reason a refused sign-in came back with (?sso_error=).
afterEach(() => {
  vi.unstubAllGlobals();
  window.history.pushState({}, "", "/");
});

function mount(status: Record<string, unknown>, path = "/login") {
  window.history.pushState({}, "", path);
  vi.stubGlobal("fetch", vi.fn((req: Request) =>
    Promise.resolve(new URL(req.url).pathname.endsWith("/auth/status") ? Response.json(status) : new Response("{}", { status: 404 })),
  ));
  const qc = new QueryClient();
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={() => <Login />} />
      </Router>
    </QueryClientProvider>
  ));
}

describe("Login", () => {
  it("offers single sign-on when a provider is configured", async () => {
    mount({ bootstrapped: true, lab: false, sso_providers: ["default"] });
    expect(await screen.findByRole("button", { name: "Sign in with SSO" })).toBeInTheDocument();
    expect(screen.getByLabelText("Username")).toBeInTheDocument();
  });

  it("offers no single sign-on when none is configured", async () => {
    mount({ bootstrapped: true, lab: false, sso_providers: [] });
    expect(await screen.findByLabelText("Username")).toBeInTheDocument();
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.queryByRole("button", { name: "Sign in with SSO" })).toBeNull();
  });

  it("shows the reason a refused sign-in came back with", async () => {
    mount({ bootstrapped: true, lab: false, sso_providers: ["default"] }, "/login?sso_error=Your%20identity%20is%20not%20linked");
    expect(await screen.findByRole("alert")).toHaveTextContent("Your identity is not linked");
  });
});
