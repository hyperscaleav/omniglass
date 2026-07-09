import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { api, setUnauthorizedHandler } from "./client";

// The global 401 interceptor: a 401 from a PROTECTED route means a session that was
// valid has ended (expired, reset, locked out), so it fires the registered handler
// (which the app uses to null the principal and bounce to /login). A 401 from an
// auth endpoint is that endpoint's own normal outcome and must NOT fire it.
describe("client 401 interceptor", () => {
  let fired: number;

  beforeEach(() => {
    fired = 0;
    setUnauthorizedHandler(() => { fired++; });
  });
  afterEach(() => {
    setUnauthorizedHandler(() => {});
    vi.restoreAllMocks();
  });

  function stub401() {
    vi.stubGlobal("fetch", vi.fn(async () => new Response(null, { status: 401 })));
  }

  it("fires the handler on a 401 from a protected route", async () => {
    stub401();
    await api.GET("/principals");
    expect(fired).toBe(1);
  });

  it("does not fire on a 401 from /auth/me (an anonymous read is normal)", async () => {
    stub401();
    await api.GET("/auth/me");
    expect(fired).toBe(0);
  });

  it("does not fire on a 401 from /auth/login (a bad password is normal)", async () => {
    stub401();
    await api.POST("/auth/login", { body: { username: "x", password: "y" } });
    expect(fired).toBe(0);
  });

  it("does not fire on a successful protected request", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("[]", { status: 200, headers: { "Content-Type": "application/json" } })));
    await api.GET("/principals");
    expect(fired).toBe(0);
  });
});
