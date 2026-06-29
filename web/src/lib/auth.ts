import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { api, setToken, clearToken } from "../api/client";

// Auth for the SPA. The backend is bearer-only: GET /auth/me with the stored
// token resolves the principal (200) or rejects (401). "Logging in" is pasting
// a valid token; we store it, then validate by reading /auth/me.

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

// useLogin stores a pasted bearer token and validates it by reading /auth/me.
// On success it primes the cache; on 401 it clears the bad token.
export function useLogin() {
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
    qc.setQueryData(ME_KEY, data);
    await qc.invalidateQueries({ queryKey: ME_KEY });
    return { ok: true };
  };
}

export function useLogout() {
  const qc = useQueryClient();
  return async () => {
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
