import { describe, it, expect, vi, beforeEach } from "vitest";
import { renderHook } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import type { JSX } from "solid-js";
import { useLogin, useTokenLogin, useLogout, useUpdateProfile, useChangePassword } from "./auth";
import { getToken, setToken } from "../api/client";

// The auth hooks are the unit under test; fetch is the seam we fake, so these
// assert the request shape (no stale bearer rides along) and the localStorage
// side effects without a server. A fresh QueryClient backs each hook so the
// cache priming has somewhere to land.
function jsonResponse(body: unknown, status = 200): Response {
  const payload = body === null ? null : JSON.stringify(body);
  return new Response(payload, { status, headers: { "Content-Type": "application/json" } });
}

function wrapper(props: { children: JSX.Element }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={qc}>{props.children}</QueryClientProvider>;
}

const me = { principal: { id: "p1", kind: "human" }, permissions: [], grants: [] };

describe("auth hooks", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
  });

  // The double-login regression: a stale token left in localStorage must be
  // dropped before the login POST, or it shadows the session cookie the server
  // is about to set and the first attempt 401s.
  it("useLogin drops a stale token before posting so it cannot shadow the cookie", async () => {
    setToken("ogp_stale_donotuse");
    const calls: { url: string; method: string; auth: string | null }[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
      const req = input as Request;
      calls.push({ url: req.url, method: req.method, auth: req.headers.get("Authorization") });
      if (req.url.endsWith("/auth/login")) return Promise.resolve(jsonResponse(null, 204));
      if (req.url.endsWith("/auth/me")) return Promise.resolve(jsonResponse(me, 200));
      return Promise.resolve(jsonResponse({}, 404));
    });

    const { result } = renderHook(useLogin, { wrapper });
    const res = await result("dev", "dev");

    expect(res.ok).toBe(true);
    expect(getToken()).toBe(""); // the stale token was cleared
    const login = calls.find((c) => c.url.endsWith("/auth/login"));
    expect(login?.auth).toBeNull(); // no stale bearer rode along to shadow the cookie
  });

  it("useLogin reports a 401 as invalid credentials", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ detail: "bad" }, 401));
    const { result } = renderHook(useLogin, { wrapper });
    const res = await result("dev", "nope");
    expect(res).toMatchObject({ ok: false });
  });

  it("useTokenLogin stores a valid token and clears a rejected one", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(me, 200));
    const ok = await renderHook(useTokenLogin, { wrapper }).result("ogp_good_token");
    expect(ok.ok).toBe(true);
    expect(getToken()).toBe("ogp_good_token");

    vi.restoreAllMocks();
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({}, 401));
    const bad = await renderHook(useTokenLogin, { wrapper }).result("ogp_bad_token");
    expect(bad.ok).toBe(false);
    expect(getToken()).toBe(""); // a rejected token is not left behind
  });

  it("useLogout posts logout and drops the stored token", async () => {
    setToken("ogp_session_token");
    const calls: { url: string; method: string }[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation((input) => {
      const req = input as Request;
      calls.push({ url: req.url, method: req.method });
      return Promise.resolve(jsonResponse(null, 204));
    });

    await renderHook(useLogout, { wrapper }).result();

    expect(calls.some((c) => c.url.endsWith("/auth/logout") && c.method === "POST")).toBe(true);
    expect(getToken()).toBe("");
  });

  it("useUpdateProfile PATCHes the editable fields", async () => {
    let sent: { url: string; method: string; body: unknown } | null = null;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PATCH") sent = { url: req.url, method: req.method, body: await req.clone().json() };
      return jsonResponse({ username: "ops", display_name: "Ops Lead", email: "ops@new.example" }, 200);
    });

    const res = await renderHook(useUpdateProfile, { wrapper }).result({ display_name: "Ops Lead", email: "ops@new.example" });

    expect(res.ok).toBe(true);
    expect(sent!.url.endsWith("/auth/me")).toBe(true);
    expect(sent!.method).toBe("PATCH");
    expect(sent!.body).toMatchObject({ display_name: "Ops Lead", email: "ops@new.example" });
  });

  it("useUpdateProfile reports a server error", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ detail: "boom" }, 500));
    const res = await renderHook(useUpdateProfile, { wrapper }).result({ display_name: "x" });
    expect(res).toMatchObject({ ok: false });
  });

  it("useChangePassword maps 204, 403, and 422 to clear outcomes", async () => {
    // Success.
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse(null, 204));
    expect((await renderHook(useChangePassword, { wrapper }).result("old", "new-strong-pw")).ok).toBe(true);

    // Wrong current password.
    vi.restoreAllMocks();
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ detail: "current password is incorrect" }, 403));
    const wrong = await renderHook(useChangePassword, { wrapper }).result("bad", "new-strong-pw");
    expect(wrong).toMatchObject({ ok: false });
    expect((wrong as { message: string }).message).toMatch(/current password/i);

    // Too-short new password.
    vi.restoreAllMocks();
    vi.spyOn(globalThis, "fetch").mockResolvedValue(jsonResponse({ detail: "validation" }, 422));
    const short = await renderHook(useChangePassword, { wrapper }).result("old", "short");
    expect(short).toMatchObject({ ok: false });
    expect((short as { message: string }).message).toMatch(/8 characters/i);
  });
});
