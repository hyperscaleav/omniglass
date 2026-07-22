import { describe, it, expect } from "vitest";
import { PLATFORM_AUTHORITY_HINT, writeErrorMessage } from "./settings";

// Every settings write lands at the platform tier, so the server gates it on
// platform:update on top of settings:update. The console hides the controls from a
// principal missing either half, but a permission revoked mid-session still reaches
// the server, and a 403 there must read as the authority gap it is rather than a
// generic failure. The mapping is pure, so it unit-tests without a query client.
describe("settings write error mapping", () => {
  it("names the install-wide authority a 403 is really about", () => {
    expect(writeErrorMessage(403, "Could not save the setting.")).toBe(PLATFORM_AUTHORITY_HINT);
    expect(writeErrorMessage(403, "Could not save the setting.")).toContain("platform:update");
  });

  it("keeps the caller's own message for any other failure", () => {
    expect(writeErrorMessage(422, "Could not save the setting.")).toBe("Could not save the setting.");
    expect(writeErrorMessage(500, "Could not restore defaults.")).toBe("Could not restore defaults.");
    expect(writeErrorMessage(0, "Could not restore the namespace.")).toBe("Could not restore the namespace.");
  });
});
