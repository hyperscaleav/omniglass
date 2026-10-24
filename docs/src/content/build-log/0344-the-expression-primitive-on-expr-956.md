---
title: "The expression primitive on Expr (#956)"
date: 2026-10-21
refs: [956]
---

The first slice of the expression engine (#524): `internal/expression`, on Expr
(`github.com/expr-lang/expr` v1.17.8, MIT). A site declares its environment as a Go struct and
pins its result type (`EnvOf[T](expression.Bool)` for a criteria); the binding names, types and
docs come from the struct's fields and `expr` / `doc` tags (`Env.Bindings()`), which is what the
generated bindings list will read. `Compile` refuses an unknown binding, a result of the wrong
type (including one whose type is only known at run time), and any `$` name (Expr's dynamic
`$env` and the engine's own plumbing), each as an `*expression.Error` with a phase and a 1-based
line and column. `Program.Run` takes one prepared input of the site's type.

A site also names the builtins it admits and its run budget (`WithBuiltins`, `WithBudget`).
Builtins are an **allowlist**: every Expr builtin is off, and a site may admit only vetted ones,
each linear in what the run is charged for with output no larger than its inputs. Two adversarial
review passes shaped this. The first found walks the budget did not charge (string `+`, variadic
arguments, comparisons, `contains` and `matches` subjects); those are charged, at least one per
step. The second found the charging model cannot bound Go-native builtins at all: `repeat`
allocates before any check, aliased `map` builds 10^9 leaves for a few units that `max`, `toJSON`
and `flatten` walk natively for seconds, `uniq` is quadratic, `replace` and `join` output a product
of their inputs, and the deadline cannot interrupt a native call. So the criteria set is
comparison, arithmetic, `in`, `contains` / `startsWith` / `endsWith`, constant-pattern `matches`,
`len`, the numeric builtins on scalars only, the trim and case builtins, and `all` / `any` /
`none` / `one` / `count` / `filter` with no nested predicate; the range `..`, the builders and
encoders, and literals past 64 elements are refused at compile time, with a position. Each later
site vets a builtin deliberately, with its cost argument and tests. A third pass found two costs
the charge understated: RE2 is linear in the subject times the compiled pattern
(`(a|aa|aaa|b){0,1000}z` over 1000 characters took 643ms on a charge of 1000), so a pattern is
capped at 256 compiled instructions and a match charged length times size; and equality on a
struct, a map, or a list of lists falls to a deep comparison (200ms for 2 units), so `==`, `!=`
and `in` take scalars and lists of scalars only, checked on the typed tree. `count` without a
predicate needs a list of bools at compile time, and the sandbox's settings (`CriteriaBudget`,
`CeilingBudget`, `CriteriaBuiltins`, `DisabledBuiltins`) are functions returning fresh copies, the
ceiling built from constants.

The caps: a 4096-byte source, 1000 AST nodes (counted by walking the tree), and a per-site budget
(criteria: 10,000 walked, 10,000 allocated, a 5ms deadline; ceiling: 1,000,000, 100,000, 250ms).
The deadline is a backstop for charged work and the one nondeterministic limit. `now`, `date`,
`duration` and `timezone` can never be vetted; an expression calls builtins only (no method, no
bound value); and an environment type carrying a func, a channel, an interface (`any` included),
or methods is refused when the site is declared, so a future JSON-payload site needs a dedicated
data-only value type. Integer arithmetic wraps on overflow, as Go's does. Compiled programs are
cached by (source, env, including its builtins and budget) in a 1024-entry LRU safe for concurrent
use. The expressions page keeps its Design badge until the criteria entry point lands.
