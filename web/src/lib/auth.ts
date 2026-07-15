import { useQuery, useQueryClient, type QueryClient } from "@tanstack/solid-query";
import { api, setToken, clearToken } from "../api/client";

// Auth for the SPA. Authentication is an httpOnly session cookie set by
// POST /auth/login; GET /auth/me resolves the principal (200) or rejects (401).
// The SPA never sees the token: it logs in with a username and password, and the
// cookie rides on every request (the client sends credentials).

export type Me = {
  principal: { id: string; kind: string };
  human?: { username: string; email?: string; display_name?: string; must_change_password?: boolean; has_avatar?: boolean };
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
    if (response.status === 403) {
      // The password was correct but the account is disabled (a distinct 403).
      return { ok: false, message: "This account is disabled. Contact your administrator." };
    }
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

// useUpdateProfile patches the caller's own display name (email is set by an
// administrator, not here), then invalidates /auth/me so the console reflects the
// change everywhere it is shown.
export function useUpdateProfile() {
  const qc = useQueryClient();
  return async (patch: { display_name?: string }): Promise<{ ok: true } | { ok: false; message: string }> => {
    const { error } = await api.PATCH("/auth/me", { body: patch });
    if (error) {
      return { ok: false, message: "Could not save your profile." };
    }
    await qc.invalidateQueries({ queryKey: ME_KEY });
    return { ok: true };
  };
}

// useChangePassword verifies the current password and sets a new one. A wrong
// current password is a 403, a too-short new one a 422; both map to a clear
// message.
export function useChangePassword() {
  const qc = useQueryClient();
  return async (current: string, next: string): Promise<{ ok: true } | { ok: false; message: string }> => {
    const { error, response } = await api.POST("/auth/me:changePassword", {
      body: { current_password: current, new_password: next },
    });
    if (response.status === 403) {
      return { ok: false, message: "Your current password is incorrect." };
    }
    if (response.status === 422) {
      return { ok: false, message: "New password must be at least 8 characters." };
    }
    if (error) {
      return { ok: false, message: "Could not change your password." };
    }
    // Refresh /auth/me so a cleared must_change_password flag releases the
    // force-change gate (and any other principal state stays fresh).
    await qc.invalidateQueries({ queryKey: ME_KEY });
    return { ok: true };
  };
}

// Read a File as a base64 string (no data: prefix).
export function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const s = String(reader.result);
      resolve(s.slice(s.indexOf(",") + 1));
    };
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(file);
  });
}

// setMyAvatar uploads a chosen image file as the caller's profile picture. The
// server normalizes it to a 256x256 JPEG; a bad or oversize image is a 422 whose
// detail surfaces to the operator.
export async function setMyAvatar(file: File): Promise<{ ok: boolean; message: string }> {
  const image_base64 = await fileToBase64(file);
  const { error } = await api.POST("/auth/me:setAvatar", { body: { image_base64 } });
  return error ? { ok: false, message: (error as { detail?: string }).detail ?? "Upload failed." } : { ok: true, message: "" };
}

// removeMyAvatar clears the caller's profile picture (a no-op if none is set).
export async function removeMyAvatar(): Promise<{ ok: boolean; message: string }> {
  const { error } = await api.POST("/auth/me:removeAvatar", {});
  return error ? { ok: false, message: "Remove failed." } : { ok: true, message: "" };
}

// fetchMyAvatar reads the caller's profile picture and returns it as a data URL,
// or null when there is no picture (a 404).
export async function fetchMyAvatar(): Promise<string | null> {
  const { data, error } = await api.GET("/auth/me/avatar", {});
  if (error || !data) return null;
  return `data:image/jpeg;base64,${data.image_base64}`;
}

// sensitiveResources mirror the server's set (internal/rbac/rbac.go): resources a
// bare single-token `*` wildcard does not reach, in both the direct match and the
// `:read` floor, so the viewer floor (`*:read`) cannot enumerate them. A literal
// grant, a `<resource>:*`, and owner's `>` still name them. `secret` is here so a
// field tech does not see the platform-credential directory (per-secret
// admin_sensitivity, enforced server-side, then fences individual rows). Keep this
// in sync with the Go set.
const SENSITIVE_RESOURCES = new Set(["secret"]);

// A permission is a colon-delimited topic pattern, matched exactly like the server
// rbac core that authorizes the request: a literal matches itself, `*` matches
// exactly one token, and `>` matches one or more trailing tokens (and is last). A
// two-token pattern therefore cannot reach a three-token `:admin` permission, so
// sensitivity is a deeper token, not a special case. The action token may be a
// comma list ("create,update,delete"), which expands to one pattern per action; a
// malformed permission (an empty token) is dropped so it cannot widen access.
function permPatterns(perms: string[]): string[][] {
  const out: string[][] = [];
  for (const perm of perms) {
    const segs = perm.split(":");
    if (segs.some((s) => s === "")) continue; // malformed: grants nothing
    if (segs.length === 1) {
      if (segs[0] === ">") out.push([">"]);
      continue; // a bare resource has no action; it grants nothing
    }
    for (const raw of segs[1].split(",")) {
      const act = raw.trim();
      if (act) out.push([segs[0], act, ...segs.slice(2)]);
    }
  }
  return out;
}

// matchTopic mirrors the server's match: `>` covers the non-empty remainder, `*`
// covers a single token, a literal covers itself, and both must exhaust together.
// A bare `*` at the resource position does not reach a sensitive resource (the
// viewer floor cannot enumerate secrets).
function matchTopic(pat: string[], path: string[]): boolean {
  let i = 0;
  for (; i < pat.length; i++) {
    if (pat[i] === ">") return path.length - i >= 1;
    if (i >= path.length) return false;
    if (pat[i] === "*") {
      if (i === 0 && SENSITIVE_RESOURCES.has(path[0])) return false;
      continue;
    }
    if (pat[i] !== path[i]) return false;
  }
  return i === path.length;
}

// can reports whether the principal's flattened permissions allow a permission,
// given as its tokens (e.g. can(me, "location", "read") or, for a sensitive tier,
// can(me, "audit", "read", "admin")). It mirrors the server's Allows, including
// the :read floor (holding any permission on a resource implies read on it, but
// only for a two-token read query, so the floor never reaches a `:admin` tier) and
// the sensitive-resource set (a bare `*` does not floor a sensitive resource, so a
// `*:read`-only viewer cannot read secrets), so the console hides exactly what the
// server would deny. A UI hint only; the server is the authority.
export function can(me: Me | null | undefined, ...tokens: string[]): boolean {
  if (!me) return false;
  const pats = permPatterns(me.permissions);
  for (const pat of pats) if (matchTopic(pat, tokens)) return true;
  if (tokens.length === 2 && tokens[1] === "read") {
    const sensitive = SENSITIVE_RESOURCES.has(tokens[0]);
    for (const pat of pats) {
      if (pat.length > 0 && (pat[0] === ">" || pat[0] === tokens[0] || (pat[0] === "*" && !sensitive))) return true;
    }
  }
  return false;
}
