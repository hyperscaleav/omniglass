import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { api } from "../api/client";

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

// useLogin posts a username and password to /auth/login. On success the server
// sets the session cookie; we then invalidate /auth/me to load the principal.
export function useLogin() {
  const qc = useQueryClient();
  return async (username: string, password: string): Promise<{ ok: true } | { ok: false; message: string }> => {
    const { error, response } = await api.POST("/auth/login", { body: { username, password } });
    if (response.status === 401) {
      return { ok: false, message: "Invalid username or password." };
    }
    if (error) {
      return { ok: false, message: "Could not reach the server." };
    }
    await qc.invalidateQueries({ queryKey: ME_KEY });
    return { ok: true };
  };
}

// useLogout posts /auth/logout (revoking the session and clearing the cookie),
// then resets the cache.
export function useLogout() {
  const qc = useQueryClient();
  return async () => {
    await api.POST("/auth/logout");
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
