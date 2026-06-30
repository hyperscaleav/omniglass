import { useQuery, useQueryClient, type QueryClient } from "@tanstack/solid-query";
import { api, setToken, clearToken } from "../api/client";

// Auth for the SPA. Authentication is an httpOnly session cookie set by
// POST /auth/login; GET /auth/me resolves the principal (200) or rejects (401).
// The SPA never sees the token: it logs in with a username and password, and the
// cookie rides on every request (the client sends credentials).

export type Me = {
  principal: { id: string; kind: string };
  human?: { username: string; email?: string; display_name?: string };
  service?: { label: string };
  permissions: string[];
  grants: { role: string; scope_kind: string; scope_id?: string }[];
};

export const ME_KEY = ["auth", "me"] as const;

// useMe is the source of truth for "am I authenticated". A 401 resolves to null
// (a normal anonymous outcome, not a retry target).
export function useMe() {
  return useQuery(() => ({
    queryKey: ME_KEY,
    queryFn: async (): Promise<Me | null> => {
      const { data, error, response } = await api.GET("/auth/me");
      if (response.status === 401) return null;
      if (error) throw error;
      return data as Me;
    },
    staleTime: 30_000,
    retry: false,
  }));
}

// primeMe loads the principal into the cache after a successful sign-in, BEFORE
// the caller navigates: it reads /auth/me (now authenticated) and writes the
// result, so the auth guard sees the principal immediately and does not bounce
// back to the login screen on the first attempt.
async function primeMe(qc: QueryClient): Promise<void> {
  const { data } = await api.GET("/auth/me");
  qc.setQueryData(ME_KEY, (data as Me) ?? null);
  await qc.invalidateQueries({ queryKey: ME_KEY });
}

// useLogin posts a username and password to /auth/login. On success the server
// sets the session cookie; we then prime /auth/me so the guard sees the principal.
export function useLogin() {
  const qc = useQueryClient();
  return async (username: string, password: string): Promise<{ ok: true } | { ok: false; message: string }> => {
    // Drop any stale bearer token so it cannot shadow the new session cookie.
    clearToken();
    const { error, response } = await api.POST("/auth/login", { body: { username, password } });
    if (response.status === 401) {
      return { ok: false, message: "Invalid username or password." };
    }
    if (error) {
      return { ok: false, message: "Could not reach the server." };
    }
    await primeMe(qc);
    return { ok: true };
  };
}

// useTokenLogin stores a pasted bearer token (the optional token path) and
// validates it by reading /auth/me; on 401 it clears the bad token.
export function useTokenLogin() {
  const qc = useQueryClient();
  return async (token: string): Promise<{ ok: true } | { ok: false; message: string }> => {
    setToken(token.trim());
    const { data, error, response } = await api.GET("/auth/me");
    if (response.status === 401) {
      clearToken();
      return { ok: false, message: "That token is not valid." };
    }
    if (error) {
      clearToken();
      return { ok: false, message: "Could not reach the server." };
    }
    qc.setQueryData(ME_KEY, data as Me);
    await qc.invalidateQueries({ queryKey: ME_KEY });
    return { ok: true };
  };
}

// useLogout posts /auth/logout (revoking the session and clearing the cookie),
// drops any stored token, and resets the cache.
export function useLogout() {
  const qc = useQueryClient();
  return async () => {
    await api.POST("/auth/logout");
    clearToken();
    qc.setQueryData(ME_KEY, null);
    await qc.invalidateQueries({ queryKey: ME_KEY });
  };
}

// can reports whether the principal's flattened permissions allow a
// resource:action, with the wildcard and :read floor the server applies. A UI
// hint only; the server is the authority.
export function can(me: Me | null | undefined, resource: string, action: string): boolean {
  if (!me) return false;
  for (const perm of me.permissions) {
    const [r, a] = perm.split(":");
    if (r !== "*" && r !== resource) continue;
    if (action === "read" || a === "*" || a === action) return true;
  }
  return false;
}
