import { createSignal } from "solid-js";
import type { QueryClient } from "@tanstack/solid-query";
import { api, getToken, setToken, clearToken } from "../api/client";

// The impersonation client: an admin views/acts as another principal. The server
// mints a bearer token; we swap it in for the current session (remembering the
// real admin token to restore on stop) and drive a persistent acting-as banner.
// The server is the authority: it enforces the escalation guard, view-as
// read-only, and the dual-actor audit; this only manages the token swap and hint.
const REAL_TOKEN_KEY = "og-token-real";
const META_KEY = "og-impersonating";

export type ActingAs = { target: string; mode: "view_as" | "act_as" };

function readMeta(): ActingAs | null {
  const raw = localStorage.getItem(META_KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as ActingAs;
  } catch {
    return null;
  }
}

const [actingAs, setActingAs] = createSignal<ActingAs | null>(readMeta());

// actingAs is the reactive acting-as state (null when not impersonating).
export { actingAs };

// impersonate starts a view-as / act-as session as the target principal: it mints
// the token, saves the real admin token, swaps to the impersonation token, and
// refetches everything so the app renders as the target.
export async function impersonate(
  qc: QueryClient,
  id: string,
  targetName: string,
  mode: "view_as" | "act_as",
): Promise<{ ok: true } | { ok: false; message: string }> {
  const { data, error, response } = await api.POST("/principals/{id}:impersonate", {
    params: { path: { id } },
    body: { mode },
  });
  if (error || !data?.token) {
    if (response.status === 403) return { ok: false, message: "You cannot impersonate this principal (it exceeds your authority)." };
    if (response.status === 422) return { ok: false, message: "You cannot impersonate yourself." };
    return { ok: false, message: "Could not start impersonation." };
  }
  localStorage.setItem(REAL_TOKEN_KEY, getToken());
  setToken(data.token);
  const meta: ActingAs = { target: targetName, mode };
  localStorage.setItem(META_KEY, JSON.stringify(meta));
  setActingAs(meta);
  await qc.invalidateQueries();
  return { ok: true };
}

// stopImpersonating ends the session server-side and restores the real admin
// token, then refetches so the app returns to the admin's own view.
export async function stopImpersonating(qc: QueryClient): Promise<void> {
  await api.POST("/auth/me:stopImpersonation", {}).catch(() => undefined);
  const real = localStorage.getItem(REAL_TOKEN_KEY);
  if (real) setToken(real);
  else clearToken();
  localStorage.removeItem(REAL_TOKEN_KEY);
  localStorage.removeItem(META_KEY);
  setActingAs(null);
  await qc.invalidateQueries();
}
