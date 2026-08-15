import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Profile from "./Profile";
import { ME_KEY, type Me } from "../lib/auth";

// The Profile page is the signed-in operator's own account surface. Its avatar
// section renders the current picture (fetched as base64 and wrapped in a data
// URL) when the principal has one, or the initials placeholder when it does not,
// with Upload and Remove controls. Data is seeded into the query cache and the
// avatar endpoints are stubbed on fetch, so no server is needed.
const jpegB64 = "/9j/4AAQSkZJRg==";

function meWith(hasAvatar: boolean): Me {
  return {
    principal: { id: "u-me", kind: "human" },
    human: { username: "ada", label: "Ada Lovelace", has_avatar: hasAvatar },
    permissions: [">"],
    grants: [],
  };
}

function mount(hasAvatar: boolean) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], meWith(hasAvatar));
  return render(() => (
    <QueryClientProvider client={qc}>
      <Profile />
    </QueryClientProvider>
  ));
}

describe("Profile avatar", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders the profile picture as an image when the principal has one", async () => {
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

  it("renders the initials placeholder when the principal has no picture", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      async () => new Response(JSON.stringify(meWith(false)), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    mount(false);
    // Initials from the label (Ada Lovelace -> AD), and no image element.
    expect(await screen.findByText("AD")).toBeTruthy();
    expect(document.querySelector('img[alt="Your profile picture"]')).toBeNull();
  });

  it("calls the remove endpoint when Remove is clicked", async () => {
    let removeCalled = false;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      const url = typeof input === "string" ? input : req.url;
      const method = typeof input === "string" ? "GET" : req.method;
      if (method === "POST" && url.includes(":removeAvatar")) {
        removeCalled = true;
        return new Response(null, { status: 204 });
      }
      if (url.includes("/auth/me/avatar")) {
        return new Response(JSON.stringify({ image_base64: jpegB64 }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify(meWith(true)), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount(true);
    const remove = await screen.findByText("Remove");
    fireEvent.click(remove);
    await waitFor(() => expect(removeCalled).toBe(true));
  });
});

describe("Profile bulk revoke", () => {
  afterEach(() => vi.restoreAllMocks());

  it("offers Revoke all for tokens and posts to :revokeAll with the purpose", async () => {
    // One session (so the sessions Revoke all is hidden, nothing but the current) and two
    // tokens (so the tokens Revoke all shows), and capture the bulk-revoke POST.
    const calls: { url: string; body: unknown }[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input instanceof URL ? input.href : (input as Request).url;
      if (url.includes(":revokeAll")) {
        const raw = typeof input === "string" || input instanceof URL ? (init?.body as string | undefined) : await (input as Request).clone().text();
        calls.push({ url, body: raw ? JSON.parse(raw) : null });
        return new Response(JSON.stringify({ revoked: 2 }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      if (url.includes("/auth/me/sessions")) {
        return new Response(JSON.stringify({ sessions: [
          { id: "s1", kind: "session", prefix: "aaa", created_at: "2026-07-13T00:00:00Z", expires_at: "2026-07-13T12:00:00Z", current: true },
          { id: "t1", kind: "token", prefix: "bbb", created_at: "2026-07-13T00:00:00Z", expires_at: "2026-10-11T00:00:00Z", current: false },
          { id: "t2", kind: "token", prefix: "ccc", created_at: "2026-07-13T00:00:00Z", expires_at: "2026-10-11T00:00:00Z", current: false },
        ] }), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify(meWith(false)), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    vi.spyOn(window, "confirm").mockReturnValue(true);

    mount(false);
    // With one (current) session there is nothing else to revoke, so only the tokens
    // section shows a Revoke all. Clicking it confirms then posts purpose=token.
    const btn = await screen.findByRole("button", { name: /revoke all/i });
    fireEvent.click(btn);
    await waitFor(() => expect(calls.length).toBe(1));
    expect(calls[0].url).toContain("/auth/me/sessions:revokeAll");
    expect(calls[0].body).toEqual({ purpose: "token" });
  });
});

// The token drawer's lifetime field, converted by #724. It carried `min="1"
// max="365"` from the day it was written and neither could ever fire: the
// drawer's Create button is portaled outside the <form>, so the browser never
// validates on the path that submits, and a 400-day token was refused by the
// server's 422 after a round trip.
//
// The rule is now `tokenTtlError` and it is asserted THROUGH the surface, driving
// the same rail button an operator clicks rather than calling the validator.
describe("Profile token lifetime", () => {
  afterEach(() => vi.restoreAllMocks());

  async function openTokenDrawer() {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockImplementation(
      async () => new Response(JSON.stringify(meWith(false)), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    mount(false);
    fireEvent.click(await screen.findByRole("button", { name: /create token/i }));
    const ttl = (await waitFor(() => {
      const el = document.querySelector("#tok-ttl");
      if (!el) throw new Error("no lifetime field yet");
      return el;
    })) as HTMLInputElement;
    return { ttl, fetchSpy };
  }

  // The rail's Create button, which is the drawer's own and not the page's.
  const createBtn = () =>
    screen.getAllByRole("button", { name: /create token/i }).at(-1) as HTMLButtonElement;

  it("refuses a lifetime past the cap on the path that submits", async () => {
    const { ttl, fetchSpy } = await openTokenDrawer();
    fireEvent.input(document.querySelector("#tok-desc") as HTMLInputElement, { target: { value: "ci pipeline" } });
    fireEvent.input(ttl, { target: { value: "400" } });
    expect(await screen.findByText(/cannot outlive 365 days/i)).toBeTruthy();
    expect(createBtn().disabled).toBe(true);
    const before = fetchSpy.mock.calls.length;
    fireEvent.click(createBtn());
    expect(fetchSpy.mock.calls.length).toBe(before);
  });

  it("accepts an empty lifetime, which is the server's default rather than a zero", async () => {
    const { ttl } = await openTokenDrawer();
    fireEvent.input(document.querySelector("#tok-desc") as HTMLInputElement, { target: { value: "ci pipeline" } });
    fireEvent.input(ttl, { target: { value: "" } });
    expect(createBtn().disabled).toBe(false);
  });
});
