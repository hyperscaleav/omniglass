import { api } from "../api/client";

// The secret_type registry data layer: a typed wrapper over /secret-types.
// Read-only by design: the registry is authoritative seed-owned reference data
// (the boot seed upserts it with ON CONFLICT DO UPDATE), so there are no write
// routes and no write wrappers. It stands apart from lib/location_types.ts so a
// caller without secret:read (a sensitive resource off the *:read viewer floor)
// never has this fetch on its path to the location registry (#598). The Secrets
// page's type picker reads the same key, so the two never fetch twice.

export type SecretTypeField = {
  name: string;
  type: string;
  secret: boolean;
  origin: string;
};

export type SecretType = {
  id: string;
  // The name, the operator-facing address (ADR-0062): pickers post and
  // look up by name, never the uuid.
  name: string;
  display_name: string;
  official: boolean;
  fields: SecretTypeField[];
};

export const SECRET_TYPES_KEY = ["secret_types"] as const;

export async function listSecretTypes(): Promise<SecretType[]> {
  const { data, error } = await api.GET("/secret-types");
  if (error) throw error;
  return (data?.secret_types ?? []).map((t) => ({
    id: t.id,
    name: t.name,
    display_name: t.display_name,
    official: t.official,
    fields: (t.fields ?? []) as SecretTypeField[],
  }));
}
