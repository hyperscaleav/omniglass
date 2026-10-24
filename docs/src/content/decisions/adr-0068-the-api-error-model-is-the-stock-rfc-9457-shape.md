---
title: "The API error model is the stock RFC 9457 shape"
date: 2026-07-30
status: "Accepted"
summary: "The API error model is Huma's stock RFC 9457 problem+json (`ErrorModel` with `ErrorDetail` `{location, message, value}`); the custom `code` plus `violations` envelope sketched on the API page is retired"
---

- **Date:** 2026-07-30 | **Status:** Accepted | **Pages:** [API](/architecture/api/)
- **Decision:** the API's error model is Huma's stock RFC 9457 `application/problem+json` shape: the
  `ErrorModel` (`title`, `status`, `detail`), carrying for validation an `errors` array of `ErrorDetail`
  entries, each `{location, message, value}`. The custom envelope sketched on the API page (a stable
  machine `code` plus a `violations` array of `{field, message}`) is retired.
- **Context:** the custom envelope was designed before any route existed. Today 141 routes serve the
  stock model, and the generated SPA client and the CLI already render it uniformly. A bespoke envelope
  would be cost without a driving consumer: no caller keys on an error `code`, and Huma's `ErrorDetail`
  already names the failing field through `location`.
- **Reversible:** additive. If a consumer ever needs a stable machine code, it can be added as an
  extension field on the stock model without breaking the shape.
