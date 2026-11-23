import { describe, it, expect } from "vitest";
import { SSO_ERROR_CODES } from "./sso_errors.gen";
import { SSO_ERROR_GENERIC, ssoErrorMessage } from "./ssoErrors";

// A refused single sign-on comes back as ?sso_error=<code> (#2362). The page
// shows the fixed message for a known code and one generic message for any
// other value, so nothing a link carries is ever printed on the page.
describe("ssoErrorMessage", () => {
  it("gives every code the server sends a message of its own", () => {
    const seen = new Set<string>();
    for (const code of SSO_ERROR_CODES) {
      const msg = ssoErrorMessage(code);
      expect(msg, code).toBeTruthy();
      expect(msg, code).not.toBe(SSO_ERROR_GENERIC);
      seen.add(msg!);
    }
    expect(seen.size).toBe(SSO_ERROR_CODES.length);
  });

  it("says what to do about an unlinked identity without naming one", () => {
    expect(ssoErrorMessage("unlinked")).toMatch(/not linked/);
    expect(ssoErrorMessage("unlinked")).not.toMatch(/\|/);
  });

  it("shows the generic message for a code it does not know, never the value itself", () => {
    for (const v of ["Your account was locked. Call 555-0100 to restore it.", "nope", "UNLINKED", "toString", "__proto__", ""]) {
      expect(ssoErrorMessage(v), v).toBe(v === "" ? null : SSO_ERROR_GENERIC);
    }
  });

  it("shows nothing when there is no refusal", () => {
    expect(ssoErrorMessage(undefined)).toBeNull();
    expect(ssoErrorMessage(["unlinked", "state"])).toBe(SSO_ERROR_GENERIC);
  });
});
