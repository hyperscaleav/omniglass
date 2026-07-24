import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { api } from "../api/client";

// The settings data layer: thin typed wrappers over the generated client for the
// settings engine (issue #271). The read side carries provenance (which cascade
// level a key resolved from) and lock state; the write side is an RFC 7386 merge
// patch per namespace, a namespace restore, and a factory reset. Slice-0 acts on
// the platform scope only. The client-safe /settings/me feeds the SPA theme.

// A settings document is namespace to key to value; values stay generic (the
// engine merges presence, not typed fields).
export type SettingsDoc = Record<string, Record<string, unknown>>;

// SettingsRead is the admin read: the effective document plus per-key provenance
// (sources) and lock levels, both keyed by "namespace.key".
export type SettingsRead = {
  values: SettingsDoc;
  sources: Record<string, string>;
  locks: Record<string, string>;
};

// SettingsMe is the client-safe read: effective values for client-visible
// namespaces only, no provenance.
export type SettingsMe = { values: SettingsDoc };

export const SETTINGS_KEY = ["settings"] as const;
export const SETTINGS_ME_KEY = ["settings", "me"] as const;

// useSettings reads the effective settings with provenance and lock state. Gated
// server-side by settings:read (admin); the console route also gates it.
export function useSettings() {
  return useQuery(() => ({
    queryKey: SETTINGS_KEY,
    queryFn: async (): Promise<SettingsRead> => {
      const { data, error } = await api.GET("/settings");
      if (error) throw error;
      return data as SettingsRead;
    },
  }));
}

// Every settings write lands at the platform tier, the least-specific level of the
// cascade, so the server gates all three on platform:update on top of
// settings:update. The console hides the controls from a principal missing either
// half, but a grant revoked mid-session still reaches the server: a 403 then reads
// as the authority gap it is, not as a generic save failure.
export const PLATFORM_AUTHORITY_HINT =
  "A setting applies to the whole install, so changing one needs the platform:update permission as well as settings:update.";

// writeErrorMessage maps a failed settings write to what the operator should read:
// the install-wide authority hint on a 403, the caller's own wording otherwise. Pure,
// so the mapping is unit-tested without a query client.
export function writeErrorMessage(status: number, fallback: string): string {
  return status === 403 ? PLATFORM_AUTHORITY_HINT : fallback;
}

// useSettingsMe reads the caller's effective settings (client-visible namespaces
// only). Authn-only, so any signed-in principal may read it; it feeds the theme.
export function useSettingsMe() {
  return useQuery(() => ({
    queryKey: SETTINGS_ME_KEY,
    queryFn: async (): Promise<SettingsMe> => {
      const { data, error } = await api.GET("/settings/me");
      if (error) throw error;
      return data as SettingsMe;
    },
  }));
}

// usePatchNamespace merge-patches a namespace's platform override, then invalidates
// the read (and /settings/me, since a client-visible change re-themes the SPA). A
// null value on a key restores that one key to the lower layer.
export function usePatchNamespace() {
  const qc = useQueryClient();
  return async (
    namespace: string,
    patch: Record<string, unknown>,
  ): Promise<{ ok: true } | { ok: false; message: string }> => {
    const { error, response } = await api.PATCH("/settings/{namespace}", {
      params: { path: { namespace } },
      body: patch,
    });
    if (error) return { ok: false, message: writeErrorMessage(response.status, "Could not save the setting.") };
    await qc.invalidateQueries({ queryKey: SETTINGS_KEY });
    await qc.invalidateQueries({ queryKey: SETTINGS_ME_KEY });
    return { ok: true };
  };
}

// useRestoreNamespace drops a namespace's platform override, restoring the file layer
// and the declared defaults, then invalidates the read and /settings/me.
export function useRestoreNamespace() {
  const qc = useQueryClient();
  return async (namespace: string): Promise<{ ok: true } | { ok: false; message: string }> => {
    const { error, response } = await api.DELETE("/settings/{namespace}", {
      params: { path: { namespace } },
    });
    if (error) return { ok: false, message: writeErrorMessage(response.status, "Could not restore the namespace.") };
    await qc.invalidateQueries({ queryKey: SETTINGS_KEY });
    await qc.invalidateQueries({ queryKey: SETTINGS_ME_KEY });
    return { ok: true };
  };
}

// useRestoreAllDefaults removes every platform override (a factory reset), then
// invalidates the read and /settings/me.
export function useRestoreAllDefaults() {
  const qc = useQueryClient();
  return async (): Promise<{ ok: true } | { ok: false; message: string }> => {
    const { error, response } = await api.POST("/settings:restoreDefaults");
    if (error) return { ok: false, message: writeErrorMessage(response.status, "Could not restore defaults.") };
    await qc.invalidateQueries({ queryKey: SETTINGS_KEY });
    await qc.invalidateQueries({ queryKey: SETTINGS_ME_KEY });
    return { ok: true };
  };
}
