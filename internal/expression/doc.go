// Package expression is Omniglass's one expression language: a sandboxed
// engine on Expr (github.com/expr-lang/expr) behind every operator-authored
// expression leaf (an event rule's criteria first; extractor values, step
// guards, calc and scope escapes, and filters as their epics land).
//
// A consumer site declares its environment as a Go struct type, pins the
// type its expressions must produce, names the builtins it admits, and sets
// its run budget (EnvOf, WithBuiltins, WithBudget). Compile parses,
// type-checks against that declaration, and refuses anything outside it with
// a positioned *Error: an unknown binding, a result of the wrong type, a
// builtin the site does not admit, a source or tree past its cap.
// Program.Run evaluates one prepared input of the site's type under the
// site's budget.
//
// Builtins are an allowlist, not a denylist. Every Expr builtin is off; a
// site admits only vetted ones, each linear in what the run is charged for
// and with output no larger than its inputs. Builders and encoders (map,
// reduce, repeat, replace, join, uniq, flatten, toJSON, the range ..) are
// never admitted: their cost is Go-native work that no input charge bounds
// and no deadline can pre-empt. Predicates may not nest, and literals are
// short. A later site vets a builtin deliberately, with its cost argument
// and tests.
//
// The engine is pure. It reads no clock, timezone database, randomness or
// process environment (DisabledBuiltins can never be vetted), the
// environment carries data only (no funcs, channels, interfaces, or
// methods), an expression calls builtins only, and an operator value bound in
// the environment is evaluated as data, never spliced into source. Compiled
// programs are cached by (source, env), the env including its builtins and
// budget.
//
// A run is bounded by its site's deterministic budget: what it allocates
// (Budget.Memory) and what it walks (Budget.Iterations); a criteria gets
// CriteriaBudget, and no site more than CeilingBudget. The wall-clock
// Budget.Deadline is only a backstop for charged work: it is the one limit
// that depends on the machine, and it cannot interrupt a native call, which
// is why the allowlist exists.
//
// Integer arithmetic is Expr's, which is Go's int: it wraps on overflow
// rather than failing (MaxInt + 1 is MinInt).
package expression
