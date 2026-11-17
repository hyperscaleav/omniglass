import { api } from "../api/client";

// OAuth consent for MCP clients (#2048). The server's /oauth/authorize checks
// the client's request and sends the person here with it in the query string;
// the consent page shows who is asking and which roles it may act under, and
// the person's answer goes back as a code (or a refusal) on the client's own
// redirect. The token the client then exchanges is an agent token: it holds
// only the approved roles AND the person's own grants, under the agent ceiling.

// ConsentRequest is the client's request as the server forwarded it.
export type ConsentRequest = {
  client_id: string;
  redirect_uri: string;
  code_challenge: string;
  code_challenge_method: "S256";
  state: string;
  resource: string;
};

// consentRequest reads the forwarded request from the query string, or null
// when a field the consent needs is missing (a hand-typed or truncated URL).
export function consentRequest(q: Record<string, string | string[] | undefined>): ConsentRequest | null {
  const one = (k: string) => {
    const v = q[k];
    return typeof v === "string" ? v : Array.isArray(v) ? (v[0] ?? "") : "";
  };
  const r = {
    client_id: one("client_id"),
    redirect_uri: one("redirect_uri"),
    code_challenge: one("code_challenge"),
    code_challenge_method: one("code_challenge_method"),
    state: one("state"),
    resource: one("resource"),
  };
  if (!r.client_id || !r.redirect_uri || !r.code_challenge || r.code_challenge_method !== "S256" || !r.resource) return null;
  return { ...r, code_challenge_method: "S256" };
}

// redirectHost names where the answer goes, for the person to recognize: the
// host and port, which for a desktop client is a loopback address.
export function redirectHost(uri: string): string {
  try {
    return new URL(uri).host;
  } catch {
    return uri;
  }
}

export type OAuthClient = { id: string; client_name: string; redirect_uris: string[]; roles: string[]; metadata_host?: string };

// fetchOAuthClient reads the registered client and the roles the caller may
// approve for it; null when no such client is registered.
export async function fetchOAuthClient(id: string): Promise<OAuthClient | null> {
  const { data, error, response } = await api.GET("/auth/oauth-clients/{id}", { params: { path: { id } } });
  if (response.status === 404) return null;
  if (error || !data) throw new Error("Could not read the client.");
  return { id: data.id, client_name: data.client_name, redirect_uris: data.redirect_uris ?? [], roles: data.roles ?? [], metadata_host: data.metadata_host };
}

// answerConsent sends the person's answer and returns where the browser goes
// next: the client's redirect, carrying a code or access_denied.
export async function answerConsent(
  req: ConsentRequest,
  approved: boolean,
  roles: string[],
): Promise<{ ok: true; redirect_to: string } | { ok: false; message: string }> {
  const { client_id, ...rest } = req;
  const { data, error } = await api.POST("/auth/oauth-clients/{id}:authorize", {
    params: { path: { id: client_id } },
    body: { ...rest, approved, roles: approved ? roles : [] },
  });
  if (error || !data) {
    const detail = (error as { detail?: string } | undefined)?.detail;
    return { ok: false, message: detail ?? "Could not record your answer." };
  }
  return { ok: true, redirect_to: data.redirect_to };
}
