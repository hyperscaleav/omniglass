// Package expression is Omniglass's one expression language: a sandboxed
// engine on Expr (github.com/expr-lang/expr) behind every operator-authored
// expression leaf (an event rule's criteria first; extractor values, step
// guards, calc and scope escapes, and filters as their epics land).
//
// A consumer site declares its environment as a Go struct type and pins the
// type its expressions must produce (EnvOf). Compile parses, type-checks
// against that declaration, and refuses anything outside it with a
// positioned *Error: an unknown binding, a result of the wrong type, a source
// or tree past its cap. Program.Run evaluates one prepared input of the
// site's type under a bounded run budget.
//
// The engine is pure. It reads no clock, timezone database, randomness or
// process environment (DisabledBuiltins lists what is turned off), the
// environment carries data only (no funcs, channels, or methods), and an
// operator value bound in the environment is evaluated as data, never
// spliced into source. Compiled programs are cached by (source, env shape).
package expression
