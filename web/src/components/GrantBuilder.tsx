import { For, Show, createEffect, createMemo, createSignal, createUniqueId, on, onMount } from "solid-js";
import { flattenTree, type TreeNode } from "../lib/treeselect";
import {
  chipStates,
  draftFromGrants,
  grantKey,
  isDirty,
  pendingDiff,
  stageGrant,
  toggleGrant,
  validateStage,
  SCOPE_OPS,
  TREE_OP,
  type ExistingGrant,
  type GrantRef,
  type ScopeOp,
} from "../lib/grantdraft";
import type { ScopeKind } from "../lib/principals";
import { describeError } from "../lib/format";
import { Save, X } from "./icons";

// GrantBuilder is the filter-bar-style scope editor: a keyboard-staged combobox
// (role -> scope kind -> entity) builds a chip, and the whole set of changes is
// staged locally and applied only on Save (stage -> preview -> save, so there are
// no accidental or unclear grant edits). The staging semantics live in the pure
// grantdraft core; this is the input + chip UI over it, mirroring FilterBar's
// key -> op -> value language. I/O stays at the edge: roles, entity trees, and the
// save mutation are props, so the component tests without a server.
type ScopedKind = Exclude<ScopeKind, "all" | "group">;
const SCOPE_KINDS: ScopeKind[] = ["all", "location", "system", "component"];

// A role for the picker: the id it commits, a display label, and a title tooltip
// (the description + the permissions it grants), so an admin sees what a role
// confers while building a grant.
export type RolePick = { id: string; label: string; title: string };

type Stage = "role" | "kind" | "entity" | "op";
type Suggestion =
  | { kind: "role"; value: string; label: string; title?: string; depth?: number }
  | { kind: "scope"; value: ScopeKind; label: string; depth?: number }
  | { kind: "entity"; value: string; label: string; depth: number }
  | { kind: "op"; value: ScopeOp; label: string; hint: string; depth?: number };

const errMsg = (e: "role-required" | "entity-required" | "duplicate"): string =>
  e === "duplicate" ? "That role is already granted at that scope." : e === "entity-required" ? "Pick an entity for that scope." : "Pick a role.";

export default function GrantBuilder(props: {
  principalId: string;
  current: ExistingGrant[];
  roles: RolePick[];
  entities: (kind: ScopedKind) => TreeNode[];
  scopeName: (id: string) => string | undefined;
  canGrant: boolean;
  canRevoke: boolean;
  onSave: (diff: { adds: GrantRef[]; removes: ExistingGrant[] }) => Promise<void>;
  // When present, the builder hides its own preview / Save row and hands its commit
  // (and revert, and dirty flag) up, so a parent (a detail blade's Save) drives the
  // grant commit as part of one edit session. `commit` rethrows on failure.
  bind?: (h: { commit: () => Promise<void>; cancel: () => void; dirty: () => boolean }) => void;
}) {
  const [draft, setDraft] = createSignal<GrantRef[]>(draftFromGrants(props.current));
  const [stage, setStage] = createSignal<Stage>("role");
  const [pendRole, setPendRole] = createSignal("");
  const [pendKind, setPendKind] = createSignal<ScopedKind>("location");
  const [pendEntity, setPendEntity] = createSignal("");
  const [text, setText] = createSignal("");
  const [open, setOpen] = createSignal(false);
  const [sel, setSel] = createSignal(-1);
  const [err, setErr] = createSignal<string | null>(null);
  const [saving, setSaving] = createSignal(false);
  const listId = createUniqueId();
  let inputRef: HTMLInputElement | undefined;

  // Re-seed the draft when the principal or its server grant set changes (a switch
  // or a completed save), but not while the operator is editing locally.
  const baseline = () => props.principalId + "|" + props.current.map((g) => g.id).join(",");
  createEffect(on(baseline, () => resetAll(), { defer: true }));

  const resetStaging = () => {
    setStage("role");
    setPendRole("");
    setPendKind("location");
    setPendEntity("");
    setText("");
    setSel(-1);
  };
  const resetAll = () => {
    setDraft(draftFromGrants(props.current));
    resetStaging();
    setErr(null);
  };

  const suggestions = createMemo<Suggestion[]>(() => {
    const t = text().trim().toLowerCase();
    if (stage() === "role") {
      return props.roles
        .filter((r) => r.id.toLowerCase().includes(t) || r.label.toLowerCase().includes(t))
        .map((r) => ({ kind: "role", value: r.id, label: r.label, title: r.title }) as Suggestion);
    }
    if (stage() === "kind") {
      return SCOPE_KINDS.filter((k) => k.includes(t)).map((k) => ({ kind: "scope", value: k, label: k }) as Suggestion);
    }
    if (stage() === "op") {
      return SCOPE_OPS.filter((o) => TREE_OP[o].label.toLowerCase().includes(t) || o.includes(t)).map(
        (o) => ({ kind: "op", value: o, label: `${TREE_OP[o].glyph}  ${TREE_OP[o].label}`, hint: TREE_OP[o].hint }) as Suggestion,
      );
    }
    return flattenTree(props.entities(pendKind()))
      .filter((o) => o.label.toLowerCase().includes(t))
      .map((o) => ({ kind: "entity", value: o.value, label: o.label, depth: o.depth }) as Suggestion);
  });

  const commit = (role: string, kind: ScopeKind, scopeId?: string, op?: ScopeOp) => {
    const candidate: GrantRef = { role, scope_kind: kind, scope_id: scopeId, scope_op: op };
    const bad = validateStage(draft(), candidate);
    if (bad) {
      setErr(errMsg(bad));
      return;
    }
    setDraft(stageGrant(draft(), candidate));
    setErr(null);
    resetStaging();
    inputRef?.focus();
  };

  const accept = (i: number) => {
    const s = suggestions()[i];
    if (!s) return;
    setErr(null);
    if (s.kind === "role") {
      setPendRole(s.value);
      setStage("kind");
      setText("");
      setSel(-1);
      inputRef?.focus();
      return;
    }
    if (s.kind === "scope") {
      if (s.value === "all") {
        commit(pendRole(), "all");
        return;
      }
      setPendKind(s.value as ScopedKind);
      setStage("entity");
      setText("");
      setSel(-1);
      inputRef?.focus();
      return;
    }
    if (s.kind === "entity") {
      // The operator applies to the chosen entity: advance to the op stage, where
      // subtree is pre-selected so Enter commits the common case immediately.
      setPendEntity(s.value);
      setStage("op");
      setText("");
      setSel(0);
      inputRef?.focus();
      return;
    }
    commit(pendRole(), pendKind(), pendEntity(), s.value);
  };

  // Backspace on an empty input steps back a stage, or removes the last chip when
  // already at the role stage (mirrors FilterBar's chip backspace).
  const stepBack = () => {
    if (stage() === "op") setStage("entity");
    else if (stage() === "entity") setStage("kind");
    else if (stage() === "kind") {
      setStage("role");
      setPendRole("");
    } else {
      const states = chipStates(props.current, draft());
      const last = states[states.length - 1];
      if (last && last.kind !== "removed") setDraft(toggleGrant(props.current, draft(), grantKey(last.grant)));
    }
  };

  const onKeyDown = (e: KeyboardEvent) => {
    const sugs = suggestions();
    if (e.key === "ArrowDown" && sugs.length) {
      e.preventDefault();
      setOpen(true);
      setSel((sel() + 1) % sugs.length);
    } else if (e.key === "ArrowUp" && sugs.length) {
      e.preventDefault();
      setSel((sel() - 1 + sugs.length) % sugs.length);
    } else if (e.key === "Enter") {
      e.preventDefault();
      accept(sel() >= 0 ? sel() : 0);
    } else if (e.key === "Tab" && sugs.length) {
      e.preventDefault();
      if (sugs.length === 1) accept(0);
      else {
        setOpen(true);
        setSel((sel() + (e.shiftKey ? -1 : 1) + sugs.length) % sugs.length);
      }
    } else if (e.key === "Escape") {
      resetStaging();
      setErr(null);
    } else if (e.key === "Backspace" && text() === "") {
      e.preventDefault();
      stepBack();
    }
  };

  const placeholder = () =>
    stage() === "role"
      ? "role…"
      : stage() === "kind"
        ? "scope: all, location, system, component"
        : stage() === "op"
          ? "match: at or under, under only, just this"
          : `${pendKind()}…`;

  // A scoped chip shows the operator glyph so the scope is legible at a glance
  // (= just this, ≥ at or under, > under only); the all scope has no operator.
  const chipLabel = (g: GrantRef): string => {
    if (g.scope_kind === "all") return `${g.role} @ all`;
    const name = (g.scope_id && props.scopeName(g.scope_id)) || g.scope_id;
    return `${g.role} @ ${TREE_OP[g.scope_op ?? "subtree"].glyph} ${g.scope_kind}:${name}`;
  };

  const diff = createMemo(() => pendingDiff(props.current, draft()));
  const dirty = createMemo(() => isDirty(props.current, draft()));

  // Bound mode: a parent blade's Save drives the commit. `commit` rethrows so the
  // blade can keep edit mode open and surface the error on failure.
  onMount(() => props.bind?.({ commit: () => props.onSave(diff()), cancel: resetAll, dirty }));

  const save = async () => {
    setErr(null);
    setSaving(true);
    try {
      await props.onSave(diff());
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div>
      <div class="eyebrow mb-1.5">Role grants</div>
      {/* Chip row: existing grants (kept or marked for removal) then staged adds. */}
      <div class="flex flex-wrap items-center gap-1.5">
        <For each={chipStates(props.current, draft())} fallback={<span class="text-xs text-base-content/40">No grants yet. This principal can sign in but has no permissions.</span>}>
          {(c) => (
            <span
              class="inline-flex items-center gap-1 rounded-field border py-[3px] pl-2.5 pr-1 font-data text-[11px]"
              classList={{
                "border-primary/40 bg-primary/10": c.kind === "unchanged",
                "border-success/50 bg-success/10 text-success": c.kind === "added",
                "border-base-300 bg-base-100 text-base-content/40 line-through": c.kind === "removed",
              }}
              title={c.kind === "removed" ? "Marked for removal on save" : c.kind === "added" ? "New, applied on save" : undefined}
            >
              {chipLabel(c.grant)}
              <Show when={props.canRevoke || c.kind === "added"}>
                <Show
                  when={c.kind !== "removed"}
                  fallback={
                    <button type="button" class="ml-px inline-flex text-base-content/40 no-underline hover:text-success" aria-label={`Restore ${chipLabel(c.grant)}`} onClick={() => setDraft(toggleGrant(props.current, draft(), grantKey(c.grant)))}>+</button>
                  }
                >
                  <button
                    type="button"
                    class="ml-px inline-flex text-base-content/40 hover:text-error"
                    aria-label={`${c.kind === "added" ? "Remove staged" : "Remove"} ${chipLabel(c.grant)}`}
                    onClick={() => setDraft(toggleGrant(props.current, draft(), grantKey(c.grant)))}
                  >
                    <X size={13} />
                  </button>
                </Show>
              </Show>
            </span>
          )}
        </For>
      </div>

      {/* Staged combobox: role -> kind -> entity -> op, each commit a chip. */}
      <Show when={props.canGrant}>
        <div class="relative mt-2.5">
          <div
            class="flex flex-wrap items-center gap-1.5 rounded-field border border-base-300 bg-base-100 px-2 py-1"
            onClick={(e) => e.currentTarget === e.target && inputRef?.focus()}
          >
            <Show when={pendRole()}>
              <span class="font-data text-[11px] text-base-content/60">
                {pendRole()} @
                {stage() === "entity" ? ` ${pendKind()}:` : stage() === "op" ? ` ${pendKind()}:${(pendEntity() && props.scopeName(pendEntity())) || pendEntity()} ` : ""}
              </span>
            </Show>
            <input
              ref={inputRef}
              type="text"
              class="min-w-[140px] flex-1 bg-transparent px-0.5 py-0.5 text-sm outline-none placeholder:text-base-content/40"
              value={text()}
              placeholder={placeholder()}
              role="combobox"
              aria-label="Add a grant"
              aria-expanded={open()}
              aria-controls={listId}
              aria-autocomplete="list"
              aria-activedescendant={sel() >= 0 ? `${listId}-opt-${sel()}` : undefined}
              onInput={(e) => {
                setText(e.currentTarget.value);
                setOpen(true);
                setSel(e.currentTarget.value.trim() ? 0 : -1);
              }}
              onFocus={() => setOpen(true)}
              onBlur={() => setTimeout(() => setOpen(false), 140)}
              onKeyDown={onKeyDown}
            />
          </div>
          <Show when={open() && suggestions().length > 0}>
            <ul id={listId} role="listbox" class="absolute z-40 mt-1.5 max-h-64 w-80 overflow-auto rounded-box border border-base-300 bg-base-100 p-1.5 shadow-2xl">
              <For each={suggestions()}>
                {(s, i) => (
                  <li>
                    <button
                      id={`${listId}-opt-${i()}`}
                      role="option"
                      aria-selected={sel() === i()}
                      title={s.kind === "role" ? s.title : undefined}
                      class="flex w-full items-center gap-2 rounded-field px-2 py-1.5 text-left text-sm"
                      classList={{ "bg-primary/15": sel() === i() }}
                      style={s.kind === "entity" ? { "padding-left": `${0.5 + (s.depth ?? 0) * 0.85}rem` } : undefined}
                      onMouseDown={(e) => {
                        e.preventDefault();
                        accept(i());
                      }}
                      onMouseEnter={() => setSel(i())}
                    >
                      <span classList={{ "font-data": s.kind !== "scope" && s.kind !== "op" && s.kind !== "role" }}>{s.label}</span>
                      <Show when={s.kind === "role" && s.value !== s.label}>
                        <span class="font-data text-[11px] text-base-content/40">{(s as { value: string }).value}</span>
                      </Show>
                      <Show when={stage() === "role"}>
                        <span class="ml-auto text-xs text-base-content/40" title="hover a role to see its permissions">?</span>
                      </Show>
                      <Show when={s.kind === "op"}>
                        <span class="ml-auto pl-2 text-right text-[11px] text-base-content/40">{(s as { hint: string }).hint}</span>
                      </Show>
                    </button>
                  </li>
                )}
              </For>
            </ul>
          </Show>
        </div>
      </Show>

      <Show when={err()}>
        <p class="mt-2 text-[11px] text-error">{err()}</p>
      </Show>

      {/* Preview + save: nothing is sent until the operator commits the diff. When
          bound to a blade's Save (props.bind), the blade owns this, so it hides. */}
      <Show when={dirty() && !props.bind}>
        <div class="mt-3 flex flex-wrap items-center gap-2 rounded-box border border-base-300 bg-base-100 px-3 py-2">
          <span class="text-xs text-base-content/70">
            Pending:
            <Show when={diff().adds.length}> <span class="font-medium text-success">+{diff().adds.length} to grant</span></Show>
            <Show when={diff().adds.length && diff().removes.length}>,</Show>
            <Show when={diff().removes.length}> <span class="font-medium text-error">-{diff().removes.length} to revoke</span></Show>
          </span>
          <span class="flex-1" />
          <button type="button" class="btn btn-quiet btn-xs gap-1.5" onClick={resetAll} disabled={saving()}><X size={14} /> Cancel</button>
          <button type="button" class="btn btn-action btn-xs gap-1.5" onClick={save} disabled={saving()}>
            <Show when={saving()}><span class="loading loading-spinner loading-xs" /></Show>
            <Save size={14} /> Save grants
          </button>
        </div>
      </Show>
    </div>
  );
}
