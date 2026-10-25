## limit-debited-before-the-narrow-check (fixed; found by the #1024 review)

- **Cue:** a shared budget (a listener-wide bucket, a per-target bucket) is charged before a
  narrower per-sender limit, or before authentication, on a path a stranger can reach.
- **Failure:** a sender's refused traffic spends everyone's budget. One source posting a thousand
  times a second through its own ten-a-second bucket drained the webhook listener's shared bucket
  first, so a second sender was admitted 0 of 60 times; a leaked URL's unsigned posts spent the
  endpoint's budget and silenced its real sender.
- **Fix:** check narrowest first and charge a shared budget only for work the narrower checks
  admitted (`admit`, internal/api/webhooks.go: the source's bucket, its posts in flight, then the
  listener's bucket; the endpoint's bucket only after the scheme admits the post). Moving a shared slot after the body read must keep a bound on the bytes
  being read (the webhook route's reading budget, counted at each body's declared length).
