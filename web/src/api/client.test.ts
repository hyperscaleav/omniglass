import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { api, clearToken, setToken, setUnauthorizedHandler } from "./client";
import { getCalcRule } from "./api.gen";

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

// The path-keyed client the data layer calls (ADR-1619): it builds the request the
// way openapi-fetch did, so every caller's { data, error, response } reads the same.
describe("client requests", () => {
  let seen: Request[];

  function stub(response: () => Response) {
    seen = [];
    vi.stubGlobal("fetch", vi.fn(async (req: Request) => { seen.push(req); return response(); }));
  }
  const json = (body: unknown, status = 200) =>
    new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

  afterEach(() => {
    clearToken();
    vi.restoreAllMocks();
  });

  it("fills the route's path params, encoded, under the api base", async () => {
    stub(() => json({}));
    await api.GET("/calc-rules/{name}", { params: { path: { name: "a b/c" } } });
    expect(seen[0].method).toBe("GET");
    expect(seen[0].url).toBe(`${location.origin}/api/v1/calc-rules/a%20b%2Fc`);
  });

  it("keeps a custom method's colon after a filled path param", async () => {
    stub(() => json({}));
    await api.POST("/principals/{id}:impersonate", { params: { path: { id: "p1" } }, body: { mode: "view_as" } });
    expect(new URL(seen[0].url).pathname).toBe("/api/v1/principals/p1:impersonate");
  });

  it("writes the query, skipping an unset param", async () => {
    stub(() => json({}));
    await api.GET("/calc-rules/{name}/state", { params: { path: { name: "r" }, query: { limit: 5 } } });
    await api.GET("/auth/me/notifications", { params: { query: { unread: undefined, limit: 20 } } });
    expect(new URL(seen[0].url).search).toBe("?limit=5");
    expect(new URL(seen[1].url).search).toBe("?limit=20");
  });

  it("sends no query string when every param is unset", async () => {
    stub(() => json({}));
    await api.GET("/auth/me/notifications", { params: { query: { unread: undefined } } });
    expect(seen[0].url).toBe(`${location.origin}/api/v1/auth/me/notifications`);
  });

  it("sends the body as JSON", async () => {
    stub(() => json({}));
    await api.POST("/auth/login", { body: { username: "x", password: "y" } });
    expect(seen[0].headers.get("Content-Type")).toBe("application/json");
    expect(await seen[0].json()).toEqual({ username: "x", password: "y" });
  });

  it("sends no Content-Type without a body", async () => {
    stub(() => json({}));
    await api.GET("/principals");
    expect(seen[0].headers.get("Content-Type")).toBeNull();
  });

  it("includes credentials, and the stored bearer token when there is one", async () => {
    stub(() => json({}));
    await api.GET("/principals");
    setToken("ogp_test");
    await api.GET("/principals");
    expect(seen[0].credentials).toBe("include");
    expect(seen[0].headers.get("Authorization")).toBeNull();
    expect(seen[1].headers.get("Authorization")).toBe("Bearer ogp_test");
  });

  it("returns a success's parsed body as data", async () => {
    stub(() => json({ principals: [] }));
    const { data, error, response } = await api.GET("/principals");
    expect(data).toEqual({ principals: [] });
    expect(error).toBeUndefined();
    expect(response.status).toBe(200);
  });

  it("returns a refusal's parsed problem as error", async () => {
    stub(() => json({ title: "Forbidden", status: 403, detail: "no" }, 403));
    const { data, error, response } = await api.GET("/principals");
    expect(data).toBeUndefined();
    expect(error).toEqual({ title: "Forbidden", status: 403, detail: "no" });
    expect(response.status).toBe(403);
  });

  it("returns a refusal's text when it is not JSON", async () => {
    stub(() => new Response("bad gateway", { status: 502 }));
    const { error } = await api.GET("/principals");
    expect(error).toBe("bad gateway");
  });

  it("returns no data for a 204", async () => {
    stub(() => new Response(null, { status: 204 }));
    const { data, error, response } = await api.DELETE("/calc-rules/{name}", { params: { path: { name: "r" } } });
    expect(data).toBeUndefined();
    expect(error).toBeUndefined();
    expect(response.status).toBe(204);
  });

  it("returns no data for an empty success body", async () => {
    stub(() => new Response("", { status: 200 }));
    const { data } = await api.GET("/principals");
    expect(data).toBeUndefined();
  });
});

// ogFetch is the transport orval's generated operations send through, so one of
// them reaches the same base, credentials and token as the path-keyed client.
describe("ogFetch", () => {
  afterEach(() => {
    clearToken();
    vi.restoreAllMocks();
  });

  it("sends a generated operation under the api base with the token", async () => {
    const seen: Request[] = [];
    vi.stubGlobal("fetch", vi.fn(async (req: Request) => {
      seen.push(req);
      return new Response(JSON.stringify({ name: "r" }), { status: 200, headers: { "Content-Type": "application/json" } });
    }));
    setToken("ogp_test");
    const res = await getCalcRule("r");
    expect(seen[0].url).toBe(`${location.origin}/api/v1/calc-rules/r`);
    expect(seen[0].headers.get("Authorization")).toBe("Bearer ogp_test");
    expect(res.status).toBe(200);
    expect(res.data).toEqual({ name: "r" });
  });
});

// The client keeps openapi-fetch's typing: a route, its params and its body are
// checked against the spec at compile time. `npm run typecheck` fails if any
// expected error below stops being one; the calls are never sent.
describe("client typing", () => {
  it("refuses what the spec does not allow", () => {
    const refused = async () => {
      // @ts-expect-error a route the spec does not have
      await api.GET("/no-such-route");
      // @ts-expect-error a route with a path param needs it
      await api.GET("/calc-rules/{name}");
      // @ts-expect-error a path param the route does not have
      await api.GET("/calc-rules/{name}", { params: { path: { nope: "x" } } });
      // @ts-expect-error a required body field missing
      await api.POST("/auth/login", { body: { username: "x" } });
      // @ts-expect-error a required body missing
      await api.POST("/auth/login");
      // @ts-expect-error a query param the route does not take
      await api.GET("/auth/me/notifications", { params: { query: { bogus: 1 } } });
      const { data, error } = await api.GET("/calc-rules/{name}", { params: { path: { name: "x" } } });
      if (error) throw error;
      // @ts-expect-error data is the route's typed body (name is a string)
      const n: number = data.name;
      return n;
    };
    expect(refused).toBeTypeOf("function");
  });
});
