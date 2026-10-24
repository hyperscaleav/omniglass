import { describe, it, expect } from "vitest";
import { daysLeftWord, certTone, tlsMode, shortKey } from "./tls";

// The tls rows of the reachability breakdown (#1035): what the verification
// mode means, how long the certificate has, and the key the endpoint pins,
// each read straight off the reachability read.

describe("daysLeftWord", () => {
  it("rounds to whole days", () => {
    expect(daysLeftWord(10.02)).toBe("10 days left");
    expect(daysLeftWord(9.99)).toBe("10 days left");
    expect(daysLeftWord(1.2)).toBe("1 day left");
  });
  it("says a certificate within a day of its end expires today", () => {
    expect(daysLeftWord(0.3)).toBe("expires within a day");
  });
  it("says how long ago an expired certificate ended", () => {
    expect(daysLeftWord(-0.2)).toBe("expired");
    expect(daysLeftWord(-3.4)).toBe("expired 3 days ago");
    expect(daysLeftWord(-1)).toBe("expired 1 day ago");
  });
});

describe("certTone", () => {
  it("is healthy with a month or more left, a warning inside it, bad once expired", () => {
    expect(certTone(397)).toBe("ok");
    expect(certTone(30)).toBe("ok");
    expect(certTone(29.5)).toBe("warn");
    expect(certTone(0)).toBe("bad");
    expect(certTone(-2)).toBe("bad");
    expect(certTone(undefined)).toBe("unknown");
  });
});

describe("tlsMode", () => {
  it("names each mode and marks skip as a warning", () => {
    expect(tlsMode("pin")).toMatchObject({ word: "pinned on first use", tone: "ok" });
    expect(tlsMode("verify")).toMatchObject({ word: "verified against the node's roots", tone: "ok" });
    expect(tlsMode("skip")).toMatchObject({ word: "not verified", tone: "warn" });
  });
  it("teaches what each mode trusts", () => {
    expect(tlsMode("pin").hint).toMatch(/first public key/);
    expect(tlsMode("verify").hint).toMatch(/system roots/);
    expect(tlsMode("skip").hint).toMatch(/nothing/i);
  });
  it("keeps an unknown mode visible rather than dropping it", () => {
    expect(tlsMode("bespoke").word).toBe("bespoke");
  });
});

describe("shortKey", () => {
  it("keeps the SHA256 prefix and the first characters of the digest", () => {
    expect(shortKey("SHA256:" + "A".repeat(43))).toBe("SHA256:AAAAAAAAAAAA…");
    expect(shortKey("short")).toBe("short");
  });
});
