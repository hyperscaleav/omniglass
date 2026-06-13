# Expressions: one pluggable engine, Expr first

Omniglass evaluates small operator-authored expressions in many places: a transform's
`value` and `normalize` leaves, a step's `when` guard, an `event_rule`'s fire/clear
criteria, a `calc_rule`'s reduce escape, a rule's `scope` predicate, a view/list `filter`,
and a dynamic group's membership filter. All of these go through **one pluggable expression
engine**, and the default engine is **Expr** ([expr-lang/expr](https://github.com/expr-lang/expr)).

## Pluggable from day one

The expression slot is **engine-typed**, not dialect-hardcoded. A single small interface
sits between the platform and whatever evaluates the string, so the engine can be swapped or
extended without touching the dozens of call sites or the authoring schema:

```go
// Engine is the seam. One implementation ships as the default (Expr); others
// register alongside it. The schema shape never changes when the dialect does.
type Engine interface {
    Name() string                                  // e.g. "expr"
    Compile(src string, opts CompileOpts) (Program, error)
    // Program.Eval(env) runs the compiled expression against a bound environment.
}
```

Every expression leaf carries an optional `engine` selector that defaults to `expr`. A
compiled `Program` is cached by `(engine, source, env-shape)`, so compile cost is paid once.
This is the lesson from the scratch repo: CEL had grown to ~17 hardcoded compile sites, so a
dialect change was a 17-site edit. Here there is exactly **one** swap point.

## Why Expr is engine #1

Expr is chosen for its **transform strength**: it is expression-oriented, has a rich
built-in function and operator set well suited to reshaping collected values (arithmetic,
string ops, slicing, mapping over arrays, null handling), compiles to a fast program, and is
straightforward to sandbox. CEL is predicate-oriented and weaker at the value-reshaping that
collection extractors do constantly (`raw / 100.0`, `int(groups[1])`, `node.gain`,
`groups[2] == 'true'`). **All transforms use Expr.** Where an expression is not even needed,
prefer a straightforward native path over reaching for an engine at all.

CEL is not carried forward as the standard. If a real case ever wants a second dialect, it
registers as another `Engine` implementation behind the same interface; it does not become
the default and it does not change the schema.

## Where the engine is used

| Site | Leaf | What it evaluates |
|---|---|---|
| transform extractor | `value`, `normalize` | reshape a located raw value into the typed datapoint value |
| step | `when` | the explicit branch guard (a false guard skips the step and dependents) |
| `event_rule` | `fire_criteria`, `clear_criteria` | open/close an alarm-paired event off a datapoint change |
| `calc_rule` | `reduce` (escape), `filter` | the named-reducer escape hatch and per-input filters |
| rule | `scope` | which instances a rule fires for (the CEL escape becomes the Expr escape) |
| views / list | `filter` | the structured-query predicate operators compose |
| dynamic group | membership `filter` | recomputed membership |

Because `filter` is the same engine everywhere, an operator who can write a group filter can
write a list filter and a rule scope. One language across the surface.

## In-scope bindings

Within a flow tick the engine environment exposes the documented namespaces: `$var:<key>`
(config/secret through the cascade), `$dp.<key>` (datapoints, emitted and readable for
branching), `$steps.<id>.*` (ephemeral scratch), `$event` (a listen payload), and the
extractor-local inputs a step prepares for its `value` leaf (`raw`, `groups`, `node`,
`item`). Rule and view contexts bind their own documented environments (the candidate
entity, the datapoint, the resource row).

## Safety

Expressions are **sandboxed**: no I/O, no network, no unbounded loops, bounded execution.
Operator-supplied configuration values are bound as **data in the environment**, never spliced
into expression text, so a hostile value is evaluated literally and never executed. Secret
fields rendered into a request are masked at interpolation time and never surface in a log
line, error string, or datapoint label.
