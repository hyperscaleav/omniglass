import { describe, it, expect } from "vitest";
import { deviceLabel } from "./device";

describe("deviceLabel", () => {
  it("names common browser + OS pairs", () => {
    expect(deviceLabel("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0 Safari/537.36")).toBe("Chrome on macOS");
    expect(deviceLabel("Mozilla/5.0 (Windows NT 10.0; Win64; x64) Gecko/20100101 Firefox/128.0")).toBe("Firefox on Windows");
    expect(deviceLabel("Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605 Version/17.5 Mobile Safari/604")).toBe("Safari on iOS");
    expect(deviceLabel("Mozilla/5.0 (Windows NT 10.0) AppleWebKit/537.36 Chrome/126 Safari/537.36 Edg/126.0")).toBe("Edge on Windows");
  });

  it("reads an empty UA as CLI/API (a token has no browser)", () => {
    expect(deviceLabel(undefined)).toBe("CLI / API");
    expect(deviceLabel("")).toBe("CLI / API");
  });

  it("names a CLI user-agent", () => {
    expect(deviceLabel("curl/8.4.0")).toBe("curl");
  });

  it("falls back to Unknown device for an unrecognized UA", () => {
    expect(deviceLabel("SomeRandomBot/1.0")).toBe("Unknown device");
  });
});
