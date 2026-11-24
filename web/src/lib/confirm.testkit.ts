import { createEffect, createRoot, on } from "solid-js";
import { vi } from "vitest";
import { pendingConfirm } from "./confirm_store";

// answerConfirms is a test's stand-in for the operator at the one confirm
// dialog (#2463), for a test whose subject is the page behind it, not the
// dialog (ConfirmHost.test.tsx drives the dialog itself). Each request that
// opens is answered in turn (the last answer repeats); a request with run
// is run first, as the dialog would, and answers false if run throws. The
// returned mock is called with each request's title and body, so a test can
// assert what the confirm said.
let disposers: (() => void)[] = [];

export function answerConfirms(...answers: boolean[]) {
  const asked = vi.fn<(text: string) => void>();
  let n = 0;
  createRoot((dispose) => {
    disposers.push(dispose);
    createEffect(
      on(pendingConfirm, (req) => {
        if (!req) return;
        asked(`${req.title} ${req.body ?? ""}`.trim());
        const yes = answers[Math.min(n++, answers.length - 1)] ?? false;
        if (!yes || !req.run) return req.resolve(yes);
        void req.run().then(
          () => req.resolve(true),
          () => req.resolve(false),
        );
      }),
    );
  });
  return asked;
}

export function resetAnswers() {
  disposers.forEach((d) => d());
  disposers = [];
}
