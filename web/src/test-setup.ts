import { afterEach } from "vitest";
import { resetConfirm } from "./lib/confirm_store";
import { resetAnswers } from "./lib/confirm.testkit";
import { sweepTimeouts, trackTimeouts } from "./test-timers";

trackTimeouts();

// Every test leaves the URL where it found it. A list's open drawer is a search
// param (`?open=`, #2429), so a test that opens a row would otherwise hand the
// next test in its file a page that mounts with that drawer already open.
//
// The one destructive confirm (#2463) self-mounts a host on document.body the
// first time a test asks; drop it, any open request, and any scripted
// answers between tests, so one test's dialog never answers the next one's.
afterEach(() => {
  if (typeof window !== "undefined") window.history.pushState({}, "", "/");
  resetAnswers();
  resetConfirm();
  // Registered first, so it runs after the testing library's own cleanup (hooks
  // run in reverse): the timers an unmount sets are swept too (#2637).
  sweepTimeouts();
});
