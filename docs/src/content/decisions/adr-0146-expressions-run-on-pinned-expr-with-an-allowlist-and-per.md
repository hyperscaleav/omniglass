---
title: "Expressions run on pinned Expr with an allowlist and per-site budgets, and rule criteria are expressions"
date: 2026-10-20
status: "Accepted"
summary: "Expressions run on pinned Expr behind a per-site allowlist of vetted builtins and a per-site budget (after three adversarial passes); event rule criteria are stored as `{expr}`, the short form lowered on input and by a one-time backfill, with one approved bool divergence; compile errors show on the rule; validate and preview answer from the same declarations"
---

- **Date:** 2026-10-20 | **Status:** Accepted | **Pages:** [expressions](/architecture/expressions/), [alarms and actions](/architecture/alarms-actions/), [API](/architecture/api/)
- **Decision:** The expression engine (#524) is `internal/expression` on Expr, pinned at v1.17.8, and
  six choices define it:
  1. **An allowlist, not a denylist.** Every Expr builtin is off; a site admits only builtins vetted as
     linear in what a run is charged for, with output no larger than their inputs. The range operator,
     nested predicates, builders and encoders are refused at compile time, and the clock and zone
     builtins can never be vetted. A run is bounded by its site's budget (walk, allocation, and a
     wall-clock backstop for charged work), no larger than a fixed ceiling; criteria get a small one,
     and the generated sites table on the [expressions](/architecture/expressions/) page carries each
     site's numbers. Two costs a charge on the input understated are bounded where they arise: a
     `matches` pattern must be a constant, is capped at 256 compiled regex instructions, and charges its
     subject's length times its compiled size (RE2 is linear in both); `==`, `!=` and `in` take scalars
     and lists of scalars only (and a scalar key in a map), refused at compile time on a struct, a map
     or a list of lists, whose deep comparison no charge covers.
  2. **Each site is declared once** (`internal/rules/site.go`): its Go-struct environment per value
     type, its builtins, result type and budget. The validate route, the console's expression field and
     the docs' bindings table all read that declaration; nothing lists a binding by hand.
  3. **Event rule criteria are stored as `{"expr": "..."}`.** The short form `{op, value}` stays an input
     (the API, the CLI, the console's builder) and is lowered to its expression on write, and a one-time
     data backfill lowered every stored rule the same way, byte for byte. A rule decides every sample as
     it did, with one approved divergence: a `bool` property's sample that is not exactly `true` or
     `false` holds the rule, where the short form compared its text.
  4. **A criterion that stops compiling is visible, not silent.** A compile error is cached per rule
     version like a success and carried on the rule as `criteria_error`, which the console shows; the
     rule holds until its criteria are rewritten.
  5. **Authoring is checked before saving.** `POST /expressions:validate` compiles a source against a
     site (off the shared program cache) and answers positioned diagnostics with the site's bindings;
     `POST /event-rules:preview` judges a candidate rule over recent samples in a read-only transaction,
     within the caller's read scope, sustains ignored and said so.
  6. **The label dialect stays separate** (#524, decision 1): templates render text over a
     closed map, expressions compute typed values over declared environments.
- **Context:** The first engine charged its run budget on what a run read, and three adversarial
  review passes broke it: string concatenation and variadic builtins went uncharged, Go-native builtins
  (`repeat`, aliased `map`, `uniq`, `replace`, `join`) did unbounded work no input charge could see and
  no deadline could interrupt, and regex and deep equality cost far more than their charge. Charging
  stayed for what is admitted, and the allowlist became the bound. Storing the expression rather than
  both shapes keeps one evaluator on every decide path; the backfill's lowering is proven against the
  old evaluator over every short form and sample class, and the divergence is the definition's
  decision (#524, decision 3), recorded here rather than claimed away.
