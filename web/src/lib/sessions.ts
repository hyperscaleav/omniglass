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

// The admin session surface (issue #172, slice 2): view and revoke ANOTHER
// principal's sessions from the Users blade. Gated by principal:revoke-session; the
// server bounds every read and revoke to the target principal and never leaks the
// secret. current is always false in this view (there is no "this request's own
// session" when looking at someone else), so every row reads as "Revoke".

export const principalSessionsKey = (id: string) => ["principals", id, "sessions"] as const;

// usePrincipalSessions lists a target principal's active sessions and tokens, newest
// first. The `enabled` guard lets the caller withhold the fetch until it holds the
// capability, so a viewer never fires a request the server would 403.
export function usePrincipalSessions(id: string, enabled: () => boolean = () => true) {
  return useQuery(() => ({
    queryKey: principalSessionsKey(id),
    enabled: enabled(),
    queryFn: async (): Promise<Session[]> => {
      const { data, error } = await api.GET("/principals/{id}/sessions", { params: { path: { id } } });
      if (error) throw error;
      return (data?.sessions ?? []) as Session[];
    },
  }));
}

// useRevokePrincipalSession revokes one of a target principal's sessions by id, then
// invalidates that principal's session list. A 404 means the row was already gone
// (revoked elsewhere), which is not an error to the operator.
export function useRevokePrincipalSession(id: string) {
  const qc = useQueryClient();
  return async (sid: string): Promise<{ ok: true } | { ok: false; message: string }> => {
    const { error, response } = await api.POST("/principals/{id}/sessions/{sid}:revoke", { params: { path: { id, sid } } });
    if (error && response.status !== 404) {
      return { ok: false, message: "Could not revoke that session." };
    }
    await qc.invalidateQueries({ queryKey: principalSessionsKey(id) });
    return { ok: true };
  };
}

// useRevokeAllPrincipalSessions bulk-revokes every one of a target's sessions OR every
// one of its tokens (by purpose) in a single admin action, then invalidates that
// principal's session list. Returns the count ended so the caller can report it. The
// server bounds the revoke to the target and never crosses purpose.
export function useRevokeAllPrincipalSessions(id: string) {
  const qc = useQueryClient();
  return async (purpose: "session" | "token"): Promise<{ ok: true; revoked: number } | { ok: false; message: string }> => {
    const { data, error } = await api.POST("/principals/{id}/sessions:revokeAll", { params: { path: { id } }, body: { purpose } });
    if (error) {
      return { ok: false, message: purpose === "session" ? "Could not revoke the sessions." : "Could not revoke the tokens." };
    }
    await qc.invalidateQueries({ queryKey: principalSessionsKey(id) });
    return { ok: true, revoked: data?.revoked ?? 0 };
  };
}
