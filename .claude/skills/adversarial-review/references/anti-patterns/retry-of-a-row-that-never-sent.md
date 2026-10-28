## retry-of-a-row-that-never-sent (fixed)

- **Cue:** a retry verb gated only on a terminal `failed` status that pre-send refusals (a
  predicate that did not run, a template that did not render) share with send failures.
- **Failure:** the retry sends a message that was never rendered or judged: an empty page about an
  alarm the rule never matched. Found in the #1321 review.
- **Fix:** retry only a row that made an attempt (`RetryNotification` refuses `attempts = 0`), and
  hide the retry affordance for it (`AlarmTimeline`).
