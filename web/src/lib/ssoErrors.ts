import type { SSOErrorCode } from "./sso_errors.gen";

// The words a refused single sign-on comes back with (#2362). The server
// redirects with ?sso_error=<code>, one of a closed set (lib/sso_errors.gen.ts,
// generated from internal/sso), and keeps the detail (the identity the
// provider vouched for, the provider's own error) in its log. The page prints
// only these fixed messages, so a crafted link can never put words of its own
// on the sign-in page: an unknown value shows SSO_ERROR_GENERIC.
const MESSAGES: Record<SSOErrorCode, string> = {
  not_configured: "Single sign-on is not configured on this server. Sign in with your password.",
  unreachable:
    "The identity provider is unreachable or misconfigured. Sign in with your password, or ask an administrator.",
  state: "This sign-in has expired or was already used. Start again.",
  browser: "This sign-in was not started in this browser. Start again from the sign-in page.",
  provider: "The identity provider did not sign you in. Start again, or sign in with your password.",
  token: "The identity provider's answer did not verify, so you are not signed in.",
  no_subject: "The identity provider's answer named no subject, so it cannot be matched to a person. Ask an administrator.",
  unlinked:
    "Your identity at the provider is not linked to an Omniglass person. An administrator links it from your person page, or sign in with your password and link it from your profile.",
  disabled: "Your account is disabled. Ask an administrator.",
  session: "Linking needs the session that started it. Sign in and start again from your profile.",
  already_linked: "That identity is already linked to an Omniglass person.",
  failed: "Single sign-on failed. Try again.",
};

// SSO_ERROR_GENERIC is what any value outside the closed set shows.
export const SSO_ERROR_GENERIC = "Single sign-on did not complete. Start again, or sign in with your password.";

// ssoErrorMessage is the message for a ?sso_error= value: its fixed message,
// the generic one for anything else, and null when there is none.
export function ssoErrorMessage(value: unknown): string | null {
  if (value === undefined || value === null || value === "") return null;
  if (typeof value === "string" && Object.prototype.hasOwnProperty.call(MESSAGES, value)) {
    return MESSAGES[value as SSOErrorCode];
  }
  return SSO_ERROR_GENERIC;
}
