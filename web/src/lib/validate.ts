// Shared inline-validation helpers for the IAM forms. These mirror the server's
// constraints (the Huma `pattern` / `format` on the create and update bodies, see
// internal/api/principals.go and principal_groups.go) so a form catches an invalid
// value before the round-trip: the source of truth is still the server, this is the
// same rule expressed for immediate feedback. Each returns an error message for an
// invalid value, or null when the value is acceptable (including empty, so callers
// compose with a separate `required` check).

// A handle (username or group name): lowercase letters, digits, and the separators
// dot, underscore, and hyphen, starting with a letter or digit. No capitals, no
// spaces. Mirrors pattern `^[a-z0-9][a-z0-9._-]*$`.
export const HANDLE_RE = /^[a-z0-9][a-z0-9._-]*$/;

export function handleError(value: string): string | null {
  if (!value) return null; // emptiness is a `required` concern, not a format one
  if (/\s/.test(value)) return "No spaces. Use lowercase letters, digits, and . _ -";
  if (/[A-Z]/.test(value)) return "No capital letters. Use lowercase only.";
  if (!HANDLE_RE.test(value)) return "Use lowercase letters, digits, and . _ - (start with a letter or digit).";
  return null;
}

// A minimal, permissive email shape check: one @, a dot in the domain, no spaces.
// Mirrors the server's `format: "email"`; intentionally not a full RFC 5322 parser,
// just enough to catch an obvious typo before submit.
export const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

export function emailError(value: string): string | null {
  if (!value) return null; // email is optional on a principal
  if (!EMAIL_RE.test(value)) return "Enter a valid email address (name@example.com).";
  return null;
}

// The password policy floor, kept in sync with internal/auth (MinPasswordLength).
export const MIN_PASSWORD_LENGTH = 12;

// passwordError mirrors the cheap server rules for inline feedback: a length floor
// and not containing the username. The common-password denylist stays server-side
// (too large to ship to the browser) and returns a 422 on submit, which a generated
// password never trips. Empty is not an error (a separate required/optional check
// owns emptiness). Counts characters (code points), matching the server's rune count.
export function passwordError(value: string, username?: string): string | null {
  if (!value) return null;
  if ([...value].length < MIN_PASSWORD_LENGTH) return `Use at least ${MIN_PASSWORD_LENGTH} characters.`;
  const u = (username ?? "").trim().toLowerCase();
  if (u.length >= 3 && value.toLowerCase().includes(u)) return "Must not contain the username.";
  return null;
}

// isPasswordPolicyMessage reports whether a server error message is about the
// password policy, so a form can render it inline under the password field (like the
// client checks) instead of at the head of the form. The server's policy messages
// (see mapPasswordErr in internal/api) all begin with "password"; the denylist is the
// one that reaches a submit past the inline checks.
export function isPasswordPolicyMessage(message: string | null | undefined): boolean {
  return /^password\b/i.test((message ?? "").trim());
}

// The self-service token's lifetime, mirroring `minimum:"1" maximum:"365"` on
// CreateMeTokenInputBody.ttl_days. It is the #724 conversion of a `min="1"
// max="365"` pair that had sat on the field since the drawer was written and had
// never refused anything: a Drawer's Create button is portaled outside the form,
// so the browser never validated on the path that submits.
//
// Empty is not an error, it is UNSTATED: the field is optional and the server
// applies its own default (90 days), so an empty box must reach the wire as an
// absent field rather than as a zero.
export const MAX_TOKEN_TTL_DAYS = 365;

export function tokenTtlError(value: number | ""): string | null {
  if (value === "") return null;
  if (!Number.isInteger(value) || value < 1) return "A lifetime is a whole number of days, at least 1.";
  if (value > MAX_TOKEN_TTL_DAYS) return `A token cannot outlive ${MAX_TOKEN_TTL_DAYS} days.`;
  return null;
}
