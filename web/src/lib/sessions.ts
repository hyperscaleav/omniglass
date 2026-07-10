import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { api } from "../api/client";
import { ME_KEY } from "./auth";

// Self-service sessions for the SPA (issue #172). A session or token is a bearer
// credential the caller owns; the server returns only non-secret metadata (never the
// token). A bounded expiry is a web-login "session", a null expiry a non-expiring
// CLI/API "token"; exactly one row is the current request's own credential.

export type Session = {
  id: string;
  kind: "session" | "token";
  prefix: string;
  created_at: string;
  expires_at?: string;
  current: boolean;
};

export const SESSIONS_KEY = ["auth", "me", "sessions"] as const;

// useSessions lists the caller's own active sessions and tokens, newest first.
export function useSessions() {
  return useQuery(() => ({
    queryKey: SESSIONS_KEY,
    queryFn: async (): Promise<Session[]> => {
      const { data, error } = await api.GET("/auth/me/sessions");
      if (error) throw error;
      return (data?.sessions ?? []) as Session[];
    },
  }));
}

// useRevokeSession revokes one of the caller's own sessions by id, then invalidates
// the list. It also invalidates /auth/me: revoking the CURRENT session signs it out
// (the next /auth/me resolves to null and the auth guard bounces to the login
// screen), while revoking another leaves the current principal untouched. A 404 means
// the row was already gone (revoked elsewhere), which is not an error to the user.
export function useRevokeSession() {
  const qc = useQueryClient();
  return async (id: string): Promise<{ ok: true } | { ok: false; message: string }> => {
    const { error, response } = await api.POST("/auth/me/sessions/{id}:revoke", { params: { path: { id } } });
    if (error && response.status !== 404) {
      return { ok: false, message: "Could not revoke that session." };
    }
    await qc.invalidateQueries({ queryKey: SESSIONS_KEY });
    await qc.invalidateQueries({ queryKey: ME_KEY });
    return { ok: true };
  };
}
