import { type Context, type JSX, createContext, createSignal, useContext } from "solid-js";

// The blade stack's shared state and cross-entity refs. A stack entry is a
// { kind, id } ref (not a bare node id), so one stack can hold a user blade with
// a group blade drilled over it. The registry (see BladeStack) maps a kind to the
// components that render its title and body from the id alone.
//
// Depth is bounded by construction, not by a guard here: each page roots one kind
// and drills one direction down a fixed chain (Users: user->group; Groups:
// group->user; Locations: location->child), so the drill graph is acyclic.
// `push` still folds a revisit back to its existing entry, which keeps the stack
// tidy (and cycle-safe) even if a page ever wired a two-way drill.

export type BladeRef = { kind: string; id: string };

export type BladeDef = {
  Title: (p: { id: string }) => JSX.Element;
  Body: (p: { id: string }) => JSX.Element;
  // Optional slot beside Close (e.g. Maximize on a Locations blade, or a
  // "manage in <page>" cross-over on a terminal identity blade).
  headerExtra?: (p: { id: string }) => JSX.Element;
};

export type BladeController = {
  stack: () => BladeRef[];
  push: (ref: BladeRef) => void; // truncate-to-existing on `${kind}:${id}`, else append
  pop: () => void; // drop the top blade
  close: () => void; // clear the stack
  filter: (pred: (r: BladeRef) => boolean) => void; // prune (e.g. a node deleted upstream)
  isTop: (i: number) => boolean;
};

const key = (r: BladeRef) => `${r.kind}:${r.id}`;

export function createBladeController(): BladeController {
  const [stack, setStack] = createSignal<BladeRef[]>([]);
  const push = (ref: BladeRef) =>
    setStack((s) => {
      const i = s.findIndex((r) => key(r) === key(ref));
      return i >= 0 ? s.slice(0, i + 1) : [...s, ref];
    });
  const pop = () => setStack((s) => s.slice(0, -1));
  const close = () => setStack([]);
  const filter = (pred: (r: BladeRef) => boolean) =>
    setStack((s) => {
      const next = s.filter(pred);
      return next.length === s.length ? s : next;
    });
  const isTop = (i: number) => i === stack().length - 1;
  return { stack, push, pop, close, filter, isTop };
}

export const BladesContext: Context<BladeController | undefined> = createContext<BladeController>();

export function useBlades(): BladeController {
  const c = useContext(BladesContext);
  if (!c) throw new Error("useBlades called outside a BladesContext provider");
  return c;
}
