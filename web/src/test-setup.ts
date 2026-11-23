import { afterEach } from "vitest";

// Every test leaves the URL where it found it. A list's open drawer is a search
// param (`?open=`, #2429), so a test that opens a row would otherwise hand the
// next test in its file a page that mounts with that drawer already open.
afterEach(() => {
  if (typeof window !== "undefined") window.history.pushState({}, "", "/");
});
