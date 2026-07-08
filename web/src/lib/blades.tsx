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
  // When present and true for this id, the blade offers Edit in its header: read
  // mode shows a pencil, edit mode shows Save / Cancel (see createEditSlot). The
  // body reads useBladeEdit().editing() to switch its sections read-only vs live.
  editable?: (id: string) => boolean;
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

// The per-blade edit slot: the uniform read -> Edit -> Save contract. A blade opens
// read-only; `begin` enters edit mode; the body `bind`s how to commit and revert;
// `save` runs the bound saver then leaves edit mode, `cancel` reverts and leaves.
// BladeStack owns the header chrome (the pencil, and Save / Cancel while editing);
// the detail body reads `editing` to switch its sections read-only vs live.
export type BladeEdit = {
  editable: () => boolean; // whether Edit is offered (permission + the BladeDef opts in)
  editing: () => boolean;
  saving: () => boolean;
  begin: () => void;
  cancel: () => void;
  save: () => Promise<void>;
  // The body registers its commit (and optional revert): Save runs `save`, Cancel runs `cancel`.
  bind: (h: { save: () => Promise<void>; cancel?: () => void }) => void;
};

export function createEditSlot(editable?: () => boolean): BladeEdit {
  const [editing, setEditing] = createSignal(false);
  const [saving, setSaving] = createSignal(false);
  let handler: { save: () => Promise<void>; cancel: () => void } = { save: async () => {}, cancel: () => {} };
  return {
    editable: () => (editable ? editable() : false),
    editing,
    saving,
    begin: () => setEditing(true),
    cancel: () => {
      handler.cancel();
      setEditing(false);
    },
    save: async () => {
      setSaving(true);
      try {
        await handler.save();
        setEditing(false);
      } finally {
        setSaving(false);
      }
    },
    bind: (h) => {
      handler = { save: h.save, cancel: h.cancel ?? (() => {}) };
    },
  };
}

export const BladeEditContext: Context<BladeEdit | undefined> = createContext<BladeEdit>();

export function useBladeEdit(): BladeEdit {
  const e = useContext(BladeEditContext);
  if (!e) throw new Error("useBladeEdit called outside a BladeEditContext provider");
  return e;
}
