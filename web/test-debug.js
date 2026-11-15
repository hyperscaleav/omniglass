import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { Router, Route } from "@solidjs/router";
import Profile from "./src/pages/Profile";
import { ME_KEY, type Me } from "./src/lib/auth";

function meWith(hasAvatar: boolean, email?: string): Me {
  return {
    principal: { id: "u-me", kind: "human" },
    human: { username: "ada", label: "Ada Lovelace", has_avatar: hasAvatar, email },
    permissions: [">"],
    grants: [],
  };
}

function mount(hasAvatar: boolean, email?: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], meWith(hasAvatar, email));
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={Profile} />
      </Router>
    </QueryClientProvider>
  ));
}

describe("Debug", () => {
  it("debug email rendering", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      async () => new Response(JSON.stringify(meWith(false, "ada@example.com")), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    mount(false, "ada@example.com");
    
    // Print the entire HTML
    console.log("Full HTML:");
    console.log(document.body.innerHTML);
    
    // Try to find email
    const emailText = screen.queryByText("ada@example.com");
    console.log("Email found:", emailText);
  });
});
