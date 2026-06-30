---
name: solidjs
description: Use when writing or reviewing SolidJS code in the web/ SPA (any .tsx under web/src), or reasoning about its reactivity. Solid looks like React but its model is different in ways that cause real bugs and wrong code review if you carry React intuitions over. Reach for this whenever you touch a Solid component, debug a value that will not update or an effect that will not re-run, pass or ref or mutate a DOM element, build a list or a conditional, or review someone's Solid diff. Especially load it before claiming a piece of Solid code is wrong, since the most common mistake is reviewing Solid as if it were React.
---

# SolidJS

The console SPA under `web/src` is SolidJS, not React. The JSX looks identical, so the trap is importing React mental models. Solid has no virtual DOM and no re-render: a component function runs **once**, and reactivity is fine-grained through signals and effects that update the exact DOM nodes that depend on them. Get this wrong and you write code that does not react, or you review correct code as broken.

**Stack note (so you ignore the wrong half of generic Solid guides).** This is a **plain Solid SPA**, `go:embed`-ed into the binary: **no SolidStart and no SSR**. Routing is `@solidjs/router`; server state is **TanStack Solid Query** (`useQuery`) over an `openapi-fetch` client, **not `createResource`**. So when a public Solid skill or doc leans on SolidStart, server functions, SSR/SSG, or `createResource`, that does not apply here; the data layer is TanStack Query and the thin typed `lib/*` wrappers over the generated client.

## The one that bites hardest: JSX is eager real DOM

In Solid, a native JSX element **is a real DOM node, created at the point the expression is evaluated**. It is not a description or a function to be rendered later (that is React).

```tsx
const input = <input type="text" />;   // input is an HTMLInputElement, right now
input instanceof Element;              // true
input.id = "x";                        // legal: it is a live node you can mutate
```

Consequences you must internalize:

- A native element passed as a prop or argument arrives as a live DOM node. You can `ref` it, read it, and set attributes on it before it is inserted.
- A **component** (`<Foo/>`) runs immediately and returns its root DOM node(s). `field("Parent", <TreeSelect .../>)` hands `field` the `<select>` element the component returned.
- A **fragment** (`<>...</>`) evaluates to an **array** of nodes.
- So `control instanceof Element` is **true** for a single native control, and `Array.isArray(control)` is true for a fragment. Code that keys off this (for example, generating an id and assigning `control.id` so a sibling `<label for>` can target it) works. Do not "correct" it on the React theory that JSX is a lazy description: verify against the running DOM (the accessibility tree, `element.labels`) before calling it broken.

The corollary for review: if a diff treats a JSX element as a value (refs it, mutates it, branches on `instanceof`), that is idiomatic Solid, not a bug.

## Props are reactive: do not destructure them

A component runs once, so destructuring props snapshots them and severs reactivity.

```tsx
function Badge(props: { count: number }) {
  const { count } = props;          // WRONG: count is frozen at first run
  return <span>{count}</span>;       // never updates
}
function Badge(props: { count: number }) {
  return <span>{props.count}</span>; // RIGHT: read props.x where you use it
}
```

Use `mergeProps` for defaults and `splitProps` to forward a subset, both of which preserve reactivity. The same rule applies inside effects and JSX: access `props.x` at the point of use, not once at the top.

## Signals, memos, effects

- `const [count, setCount] = createSignal(0)`: `count()` reads **and subscribes** the current tracking scope, `setCount(v)` writes. The getter is a function call, and forgetting the `()` is a common slip.
- `const total = createMemo(() => a() + b())`: a cached derived value, recomputes when its tracked deps change.
- `createEffect(() => { ... })`: runs after render and re-runs whenever a signal it read changes. Side effects only, and do not set a signal it also reads without care (loops).
- Reactivity tracks only what is **read during execution**. A signal read outside a tracking scope (an event handler, a non-reactive callback) does not subscribe. `untrack(() => ...)` reads without subscribing on purpose.

## Control flow: components, not JS operators

Solid does not re-run the component to re-render, so a bare `.map` or ternary captures values once and will not update reactively. Use the control-flow components:

- `<Show when={cond()} fallback={...}>` instead of `cond() ? a : b` for reactive branches.
- `<For each={items()}>{(item) => ...}</For>` for lists keyed by **reference** (rows move, not re-create). `<Index>` when you key by position instead.
- `<Switch>/<Match>` for multi-way.

## DOM access and escaping layout

- Refs: `let el!: HTMLDivElement; <div ref={el} />`, or a callback `ref={(e) => ...}`. The variable is assigned before `onMount`.
- `<Portal>` (from `solid-js/web`) renders children to `document.body`, escaping `overflow:hidden` and stacking contexts. This is how floating UI avoids being clipped by a card or drawer (see the [[kobalte]] skill, which portals its overlays).
- `createUniqueId()` gives a stable, SSR-safe id, the right tool for wiring `<label for>` to a control or `aria-describedby` to a tooltip.
- `onMount(fn)` runs once after first render; `onCleanup(fn)` runs on disposal (also inside effects, for teardown).

## Stores for nested state

`createSignal` holds one value; deeply nested reactive objects use `createStore`:

```tsx
const [state, setState] = createStore({ user: { name: "" }, items: [] });
setState("user", "name", "Ada");            // granular, only this leaf updates
setState("items", (it) => [...it, next]);   // or produce(draft => { draft.items.push(next) })
```

## Pitfall checklist (most are React habits)

| Symptom | Cause | Fix |
| --- | --- | --- |
| Value never updates | destructured props, or read a signal once outside JSX | read `props.x` / `sig()` at point of use |
| List does not react | used `.map` | use `<For>` |
| Branch does not react | used `? :` | use `<Show>` |
| "JSX element is not a DOM node" in review | React intuition | it IS a node in Solid; verify in the browser |
| Effect runs too much / loops | sets a signal it also reads | restructure, or `untrack` the read |
| Tooltip/menu clipped | rendered in-flow | `<Portal>` it (see [[kobalte]]) |

## When reviewing Solid

Trace behavior to the running app, not to React rules. The accessibility tree (`element.labels`, computed name), a live signal value, or a screenshot is the source of truth. Before filing a "this is broken" finding on Solid reactivity or JSX semantics, confirm it actually misbehaves in the built app.
