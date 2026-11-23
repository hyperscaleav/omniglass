import "@testing-library/jest-dom/vitest";
import { afterEach, describe, it, expect, vi } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import SelfIdentityLinks from "./SelfIdentityLinks";

// A refused self-link comes back to the profile with ?sso_error=<code>
// (#2362): the card shows that code's fixed message, and the generic one for
// any other value, never the value itself.
afterEach(() => {
  vi.unstubAllGlobals();
  window.history.pushState({}, "", "/");
});

function mount(path: string) {
  window.history.pushState({}, "", path);
  vi.stubGlobal("fetch", vi.fn((req: Request) => {
    const p = new URL(req.url).pathname;
    if (p.endsWith("/auth/status")) return Promise.resolve(Response.json({ bootstrapped: true, lab: false, sso_providers: ["default"] }));
    if (p.endsWith("/auth/me/identity-links")) return Promise.resolve(Response.json({ links: [] }));
    return Promise.resolve(new Response("{}", { status: 404 }));
  }));
  const qc = new QueryClient();
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={() => <SelfIdentityLinks />} />
      </Router>
    </QueryClientProvider>
  ));
}

describe("SelfIdentityLinks", () => {
  it("shows the message for the code a refused self-link came back with", async () => {
    mount("/profile?sso_error=already_linked");
    expect(await screen.findByRole("alert")).toHaveTextContent("That identity is already linked to an Omniglass person.");
  });

  it("shows the generic message for a value outside the codes", async () => {
    mount("/profile?sso_error=Linked%20to%20mallory");
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("Single sign-on did not complete.");
    expect(alert).not.toHaveTextContent("mallory");
  });
});
