## reason-unmasks-withheld-name (fixed; found by the #1353 review)

- **Cue:** a field is withheld from a reader (a recipient's name) while a free-text field of the same
  row, written by a third party (a relay's refusal, a receiver's error body), is passed through.
- **Failure:** the third party quotes what was withheld (`550 <alice@corp.example>: rejected`), so the
  reader learns it from the reason instead.
- **Fix:** withhold the free text with the name, and only where a name was withheld (a webhook row,
  which names no person, keeps its reason) (`RecipientView.withhold`, internal/storage/notifications.go).
