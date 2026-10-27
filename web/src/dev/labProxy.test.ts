import { describe, expect, it } from "vitest";
import { labProxyTarget } from "./labProxy";

// The console's fast loop (#1196): `npm run dev` proxies /api to a running lab.
describe("labProxyTarget", () => {
  it("defaults to the lab's console on IPv4 loopback, never localhost", () => {
    // Node may resolve localhost to ::1, where the lab does not listen.
    expect(labProxyTarget({})).toBe("http://127.0.0.1:8080");
  });
  it("follows OMNIGLASS_LAB_URL for a lab on another --port", () => {
    expect(labProxyTarget({ OMNIGLASS_LAB_URL: "http://127.0.0.1:18080" })).toBe("http://127.0.0.1:18080");
  });
  it("ignores an empty override", () => {
    expect(labProxyTarget({ OMNIGLASS_LAB_URL: "" })).toBe("http://127.0.0.1:8080");
  });
});
