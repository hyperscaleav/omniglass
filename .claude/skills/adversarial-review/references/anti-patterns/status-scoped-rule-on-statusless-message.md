## status-scoped-rule-on-statusless-message (fixed; found by the #1034 review)

- **Cue:** a rule that declares an http status, admitted on a transport whose same rules are also
  applied to messages that carry no status (a line, a websocket frame) by a classifier that
  ignores the status.
- **Failure:** the rule's match runs on its own against every answer and arm reply, so ordinary
  text in a healthy reply rejects the session and gates the endpoint until an operator retries. A
  wss driver's `{status: 403, match: {regex: locked}, class: auth}`, meant for the upgrade, rejected
  the session on a get whose value read `locked`.
- **Fix:** a classifier with no status skips status-scoped rules (`TerminalFor` in
  internal/driver/session.go; `TerminalForResponse` alone judges them); pin it with a frame that
  matches the rule's text and must not reject (`TestAStatusScopedRuleJudgesOnlyTheUpgrade`).
