import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Sidebar from "./Sidebar";
import { ME_KEY, type Me } from "../lib/auth";

// The identity footer in the rail shows the signed-in operator's own profile
// picture when they have one (fetched from the self route as base64, wrapped in a
// data URL) or their initials when they do not. Data is seeded into the query
// cache and the self avatar endpoint is stubbed on fetch, so no server is needed.
const jpegB64 = "/9j/4AAQSkZJRg==";

function meWith(hasAvatar: boolean): Me {
  return {
    principal: { id: "u-me", kind: "human" },
    human: { username: "ada", display_name: "Ada Lovelace", has_avatar: hasAvatar },
    permissions: [">"],
    grants: [],
  };
}

function mount(hasAvatar: boolean) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], meWith(hasAvatar));
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={() => <Sidebar collapsed={false} onToggle={() => {}} />} />
      </Router>
    </QueryClientProvider>
  ));
}

describe("Sidebar identity avatar", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders the operator's own picture as an image when they have one", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = typeof input === "string" ? input : (input as Request).url;
      if (url.includes("/auth/me/avatar")) {
        return new Response(JSON.stringify({ image_base64: jpegB64 }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify(meWith(true)), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount(true);
    const img = await waitFor(() => {
      const el = document.querySelector('img[alt="Your profile picture"]') as HTMLImageElement | null;
      if (!el) throw new Error("no avatar image yet");
      return el;
    });
    expect(img.src).toContain(jpegB64);
  });

  it("renders initials when the operator has no picture", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      async () => new Response(JSON.stringify(meWith(false)), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    mount(false);
    // Initials are the first two letters of the display name (uppercased by CSS, so
    // the DOM text is "Ad"), and there is no image element.
    expect(await screen.findByText("Ad")).toBeTruthy();
    expect(document.querySelector('img[alt="Your profile picture"]')).toBeNull();
  });
});

// The Inventory group carries a `section` band label on select children
// (Task 1), grouping them under a daisyUI menu-title heading in the expanded
// submenu: "Entities" over Components, "Values" over Variables.
describe("Sidebar Inventory band headers", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders the Inventory band headers (Entities and Values)", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      async () => new Response(JSON.stringify(meWith(false)), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    mount(false);
    expect(await screen.findByText("Values")).toBeTruthy();
    expect(await screen.findByText("Entities")).toBeTruthy();
  });
});
