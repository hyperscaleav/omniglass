## empty-hop-masks-composed-loss (fixed; found by the #1693 review)

- **Cue:** a figure composed over several stages (a product of survivals, an all-of check) where
  one stage can have no ratio, read as unknown for the whole; or a composition that once had one
  stage gains another and still reports when the stage that said it was settled is absent.
- **Failure:** the lab's end to end treated any hop with nothing in as unknown for its kind. A
  dispatcher that skipped every property due read schedule loss 1, the ingest row read `-`
  (nothing in, because nothing was made), and end to end printed `-`, hiding the worst loss the
  table can show. The other way: with every boot unsettled there is no ingest row, and end to end
  printed the schedule hop's ratio alone as if the kind's delivery were known.
- **Fix:** a stage with nothing in loses nothing and composes as 0; compose a kind only when the
  stage that settles it (the ingest row) is present (`EndToEndOf`, internal/labfootprint/delivery.go).
