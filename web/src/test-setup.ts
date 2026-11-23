import { afterEach } from "vitest";
import { resetConfirm } from "./lib/confirm_store";
import { resetAnswers } from "./lib/confirm.testkit";

// The one destructive confirm (#2463) self-mounts a host on document.body the
// first time a test asks; drop it, any open request, and any scripted
// answers between tests, so one test's dialog never answers the next one's.
afterEach(() => {
  resetAnswers();
  resetConfirm();
});
