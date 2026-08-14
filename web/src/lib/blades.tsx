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

// The per-blade edit slot: the uniform read -> Edit -> Save contract. A blade opens
// read-only; `begin` enters edit mode; the body `bind`s how to commit and revert;
// `save` runs the bound saver then leaves edit mode, `cancel` reverts and leaves.
// BladeStack owns the header chrome (the pencil, and Save / Cancel while editing);
// the detail body reads `editing` to switch its sections read-only vs live.
//
// Editability comes from the body, not the BladeDef: only the body knows the
// caller's permission, so it decides whether to `bind` (and passes an optional
// `editable` predicate). A body that never binds (a read-only blade like a role) is
// not editable, so no pencil renders.
// The footer action bar's non-edit slots, also registered by the body: a single
// destructive action (Delete for a group, Disable / Enable for a user), always
// available, and a list of secondary actions (Impersonate, copy id) shown in a
// kebab. Both are accessors so their label / tone stay reactive (Disable <-> Enable).
// tone: "danger" (red, a hard/irreversible action), "warn" (yellow, a reversible
// pause), or "ok" (green, a restore). The left footer slot holds one at a time.
export type BladeDestructive = { label: string; tone?: "danger" | "warn" | "ok"; onClick: () => void };
// A kebab item; tone "danger" renders it red (a destructive escalation like
// archive or purge sitting among neutral secondary actions).
export type BladeSecondary = { label: string; icon?: JSX.Element; tone?: "danger"; onClick: () => void };
// The one prominent right-side footer action for a blade whose edit is NOT the
// inline pencil model but a separate flow (an inventory row edits in a Drawer, a
// create blade submits a form): a filled button (e.g. Edit, Create interface).
// Shown only when not in inline edit. `disabled` and `busy` let a create blade
// gate its own submit on validity and spin while it is in flight, so a form
// hosted in a blade needs no buttons of its own.
export type BladePrimary = {
  label: string;
  icon?: JSX.Element;
  onClick: () => void;
  disabled?: () => boolean;
  busy?: () => boolean;
};
// The lock verdict a detail body binds (see `locked` below). Null is no lock.
// A string locks BOTH footer sides with that one reason (the official row) and
// overrides every live slot. An object locks each side independently: a side
// with a reason renders greyed carrying it, a null / absent side keeps its live
// binding, so a live Delete can sit beside a greyed Edit.
export type BladeLock = string | { edit?: string | null; delete?: string | null } | null;

export type BladeEdit = {
  editable: () => boolean;
  editing: () => boolean;
  saving: () => boolean;
  // valid gates the footer Save: false disables it, so a body with a field that
  // fails inline validation cannot be committed. Defaults to always-valid.
  valid: () => boolean;
  begin: () => void;
  cancel: () => void;
  save: () => Promise<void>;
  // Re-run the bound `seed`, for a body that replaces the ROW underneath an open
  // editor. The slot seeds on the way into edit, which covers every blade whose
  // row only changes when the operator saves it; `:restore` is the other case,
  // discarding an operator's fork so the row goes back to the shipped values
  // while the drafts still hold the fork. Left unseeded the fields show values
  // the row no longer has, and the next Save writes the discarded fork back over
  // them (#741, first seen on the location type blade in #710, where restore
  // finally sat beside a field whose value restore changes).
  //
  // It lives here rather than as a call each page remembers to make because the
  // slot is the only shared thing that knows an editor is open: "seed the drafts
  // from the row" is one concept with two triggers, and a page that binds the
  // seeder gets both.
  reseed: () => void;
  // Register an extra saver a panel contributes to the blade's Save (e.g. the
  // Fields panel flushing its staged field values). Contributors run before the
  // bound primary save, so they target the entity's current name before a rename;
  // a contributor that throws aborts the save and keeps the blade in edit mode.
  // Returns an unregister function for onCleanup.
  onSave: (saver: () => Promise<void>) => () => void;
  destructive: () => BladeDestructive | undefined;
  secondary: () => BladeSecondary[];
  primary: () => BladePrimary | undefined;
  // The lock (human copy: an official row, a missing permission). A string
  // reason greys the Edit / Delete pair in their usual spots, both carrying it
  // as a tooltip, overriding every live slot; an object greys only the sides
  // it names, each with its own reason, leaving the other side's live binding
  // in place. Null (or an unbound lock) is today's behavior exactly. Detail
  // blades only: never bind `locked` beside `primary` (a create blade under a
  // lock renders a dead form); BladeStack warns once in dev on the composition.
  locked: () => BladeLock;
  bind: (h: {
    editable?: () => boolean;
    valid?: () => boolean;
    save?: () => Promise<void>;
    cancel?: () => void;
    // Fill this body's draft signals from the row as it stands. Bound, the slot
    // runs it on entering edit (so the body needs no effect of its own) and
    // again whenever the body calls `reseed`. Unbound, nothing changes: a blade
    // that seeds with its own effect on `editing` behaves exactly as before.
    seed?: () => void;
    destructive?: () => BladeDestructive | undefined;
    secondary?: () => BladeSecondary[];
    primary?: () => BladePrimary | undefined;
    locked?: () => BladeLock;
  }) => void;
};

export function createEditSlot(): BladeEdit {
  const [editing, setEditing] = createSignal(false);
  const [saving, setSaving] = createSignal(false);
  const [bound, setBound] = createSignal(false);
  let handler: { save: () => Promise<void>; cancel: () => void } = { save: async () => {}, cancel: () => {} };
  let editablePred: () => boolean = () => false;
  let validPred: () => boolean = () => true;
  let destructivePred: () => BladeDestructive | undefined = () => undefined;
  let secondaryPred: () => BladeSecondary[] = () => [];
  let primaryPred: () => BladePrimary | undefined = () => undefined;
  let lockedPred: () => BladeLock = () => null;
  let seedFn: () => void = () => {};
  // Extra savers panels contribute (the fields panel, etc.), flushed before the
  // primary save.
  const contributors = new Set<() => Promise<void>>();
  return {
    editable: () => bound() && editablePred(),
    editing,
    saving,
    valid: () => validPred(),
    // Seed BEFORE the edit controls render, so they mount holding the row's
    // values rather than being filled a tick later.
    begin: () => {
      seedFn();
      setEditing(true);
    },
    reseed: () => seedFn(),
    cancel: () => {
      handler.cancel();
      setEditing(false);
    },
    save: async () => {
      setSaving(true);
      try {
        for (const contribute of [...contributors]) await contribute();
        await handler.save();
        setEditing(false);
      } finally {
        setSaving(false);
      }
    },
    onSave: (saver) => {
      contributors.add(saver);
      return () => contributors.delete(saver);
    },
    destructive: () => (bound() ? destructivePred() : undefined),
    secondary: () => (bound() ? secondaryPred() : []),
    primary: () => (bound() ? primaryPred() : undefined),
    locked: () => (bound() ? lockedPred() : null),
    bind: (h) => {
      // A body that supplies a saver is editable (unless it gates with `editable`);
      // one that only registers a destructive / secondary action is not.
      editablePred = h.editable ?? (() => !!h.save);
      validPred = h.valid ?? (() => true);
      handler = { save: h.save ?? (async () => {}), cancel: h.cancel ?? (() => {}) };
      destructivePred = h.destructive ?? (() => undefined);
      secondaryPred = h.secondary ?? (() => []);
      primaryPred = h.primary ?? (() => undefined);
      lockedPred = h.locked ?? (() => null);
      seedFn = h.seed ?? (() => {});
      setBound(true);
    },
  };
}

export const BladeEditContext: Context<BladeEdit | undefined> = createContext<BladeEdit>();

export function useBladeEdit(): BladeEdit {
  const e = useContext(BladeEditContext);
  if (!e) throw new Error("useBladeEdit called outside a BladeEditContext provider");
  return e;
}
