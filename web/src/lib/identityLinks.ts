import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { api } from "../api/client";

// The identity-link data layer (#2362): a person signs in by single sign-on
// only through a link to their identity at the identity provider, iss|sub.
// An admin links another person (user:link-identity, behind the takeover
// guard); a person links their own through a sign-in at the provider,
// confirmed by their current password. Thin wrappers over the generated client.

export type IdentityLink = {
  id: string;
  identifier: string;
  issuer: string;
  subject: string;
  linked_by?: string;
  self: boolean;
  created_at: string;
  last_used_at?: string;
  refresh_accepted_at?: string;
};

type Result = { ok: true } | { ok: false; message: string };

// detail reads a problem response's message, else the fallback.
function detail(error: unknown, fallback: string): string {
  const d = (error as { detail?: string } | undefined)?.detail;
  return d ? d.charAt(0).toUpperCase() + d.slice(1) + "." : fallback;
}

export const userIdentityLinksKey = (id: string) => ["users", id, "identity-links"] as const;
export const MY_IDENTITY_LINKS_KEY = ["auth", "me", "identity-links"] as const;

export function useUserIdentityLinks(id: string) {
  return useQuery(() => ({
    queryKey: userIdentityLinksKey(id),
    queryFn: async (): Promise<IdentityLink[]> => {
      const { data, error } = await api.GET("/users/{id}/identity-links", { params: { path: { id } } });
      if (error) throw error;
      return (data?.links ?? []) as IdentityLink[];
    },
  }));
}

export function useLinkIdentity(id: string) {
  const qc = useQueryClient();
  return async (identifier: string): Promise<Result> => {
    const { error } = await api.POST("/users/{id}/identity-links", { params: { path: { id } }, body: { identifier } });
    if (error) return { ok: false, message: detail(error, "Could not link that identity.") };
    await qc.invalidateQueries({ queryKey: userIdentityLinksKey(id) });
    return { ok: true };
  };
}

export function useUnlinkIdentity(id: string) {
  const qc = useQueryClient();
  return async (link: string): Promise<Result> => {
    const { error, response } = await api.DELETE("/users/{id}/identity-links/{link}", { params: { path: { id, link } } });
    if (error && response.status !== 404) return { ok: false, message: detail(error, "Could not unlink that identity.") };
    await qc.invalidateQueries({ queryKey: userIdentityLinksKey(id) });
    return { ok: true };
  };
}

export function useMyIdentityLinks() {
  return useQuery(() => ({
    queryKey: MY_IDENTITY_LINKS_KEY,
    queryFn: async (): Promise<IdentityLink[]> => {
      const { data, error } = await api.GET("/auth/me/identity-links");
      if (error) throw error;
      return (data?.links ?? []) as IdentityLink[];
    },
  }));
}

// startSelfLink confirms the password and returns the provider URL the
// browser goes to; signing in there links that identity and returns to the
// profile.
export async function startSelfLink(currentPassword: string): Promise<{ ok: true; url: string } | { ok: false; message: string }> {
  const { data, error } = await api.POST("/auth/me/identity-links:start", { body: { current_password: currentPassword } });
  if (error || !data) return { ok: false, message: detail(error, "Could not start linking.") };
  return { ok: true, url: data.url };
}

export function useUnlinkSelf() {
  const qc = useQueryClient();
  return async (link: string, currentPassword: string): Promise<Result> => {
    const { error } = await api.POST("/auth/me/identity-links/{link}:unlink", {
      params: { path: { link } },
      body: { current_password: currentPassword },
    });
    if (error) return { ok: false, message: detail(error, "Could not unlink that identity.") };
    await qc.invalidateQueries({ queryKey: MY_IDENTITY_LINKS_KEY });
    return { ok: true };
  };
}
