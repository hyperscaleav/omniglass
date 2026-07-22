import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import { X } from "./icons";
import { describeError } from "../lib/format";
import { COMPONENTS_KEY, listComponents, type Component as Comp } from "../lib/components";
import {
  addMember,
  removeMember,
  systemMembers,
  systemMembersKey,
  type Member,
} from "../lib/members";
import { systemRolesKey } from "../lib/system_roles";

// MembersPanel lists the components bound into a system. MEMBERSHIP is the
// attachment and a ROLE is what it does, so this panel answers "what is in this
// room" while the Roles panel below answers "what job does each one hold".
//
// The two are deliberately not the same list. Every component staffing a role here
// is a member (assignment creates the binding, so they can never disagree), but a
// member may hold no role at all: a power conditioner is in the room, accounted
// for, and wanted by no role. That component is invisible to a model that only
// knows about staffing, which is exactly why membership is its own relation.
//
// A member may also belong to SEVERAL systems. A divisible room's shared bar is a
// member of both halves, and each half depends on it, so the panel says so on the
// row rather than letting an operator assume this system has it to itself.
//
// Removal is the guarded write and the refusal is the lesson: the server refuses
// (409) while the component still fills a role here, because removing it would
// leave the system staffed by a non-member. The message says to unassign first,
// and it is shown verbatim.
export default function MembersPanel(props: {
  system: string;
  canUpdate: boolean;
  onOpenComponent?: (name: string) => void;
}): JSX.Element {
  const qc = useQueryClient();
  const key = () => systemMembersKey(props.system);
  const q = useQuery(() => ({
    queryKey: key(),
    queryFn: () => systemMembers(props.system),
    refetchOnWindowFocus: false,
  }));
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));

  const members = createMemo<Member[]>(() => q.data ?? []);
  const [picked, setPicked] = createSignal("");
  const [err, setErr] = createSignal<string | undefined>();
  const [busy, setBusy] = createSignal(false);

  // Only components that are not already in this system can be added, so the
  // picker never offers a no-op. It is not narrowed any further: a component
  // already serving another system is a legitimate choice, and that is the shared
  // device this whole relation exists for.
  const addable = createMemo<Comp[]>(() => {
    const held = new Set(members().map((m) => m.component));
    return (components.data ?? []).filter((c) => !held.has(c.name));
  });

  // Both caches move on a write: removing a member cannot change staffing (the
  // server refuses while a role is held), but adding one changes who the roles
  // panel can offer.
  const refresh = async () => {
    await Promise.all([
      qc.invalidateQueries({ queryKey: key() }),
      qc.invalidateQueries({ queryKey: systemRolesKey(props.system) }),
    ]);
  };

  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    setErr(undefined);
    try {
      await fn();
      await refresh();
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div class="flex flex-col gap-2">
      <div class="flex items-baseline justify-between gap-2">
        <span class="eyebrow">Members</span>
        <span class="shrink-0 text-[10.5px] text-base-content/40">what is in this system</span>
      </div>
      <p class="text-[11px] text-base-content/50">
        A component in this system. Staffing a role makes one automatically; a member may also hold no role at
        all, which is how equipment nothing has claimed is still accounted for.
      </p>

      <Show
        when={members().length}
        fallback={
          <p class="rounded-box border border-dashed border-base-300 px-3 py-3 text-center text-[12px] text-base-content/45">
            No components in this system yet.
          </p>
        }
      >
        <div class="divide-y divide-base-300 overflow-hidden rounded-box border border-base-300">
          <For each={members()}>
            {(m) => (
              <div class="flex items-center justify-between gap-2 px-3 py-2">
                <div
                  class="flex min-w-0 flex-col"
                  classList={{ "cursor-pointer": !!props.onOpenComponent }}
                  onClick={() => props.onOpenComponent?.(m.component)}
                >
                  <span class="truncate font-mono text-[12px]">{m.component}</span>
                  {/* Shared is a fact about the COMPONENT, not about this
                      binding, so it reads the count rather than the default
                      flag: a component whose default is here can still serve
                      three other systems, and inferring one from the other
                      would call that one exclusive. */}
                  <Show when={m.system_count > 1}>
                    <span class="text-[10.5px] text-warning">
                      shared with {m.system_count - 1} other system{m.system_count > 2 ? "s" : ""}
                      {m.primary ? "" : "; another holds its default"}
                    </span>
                  </Show>
                </div>
                <Show when={props.canUpdate}>
                  <Button
                    intent="quiet"
                    size="xs"
                    square
                    disabled={busy()}
                    label={`Remove ${m.component}`}
                    onClick={() => void run(() => removeMember(props.system, m.component))}
                  >
                    <X />
                  </Button>
                </Show>
              </div>
            )}
          </For>
        </div>
      </Show>

      <Show when={props.canUpdate}>
        <div class="flex items-center gap-2">
          <select
            class="select select-bordered select-sm min-w-0 flex-1"
            aria-label="Component to add"
            value={picked()}
            disabled={busy()}
            onChange={(e) => setPicked(e.currentTarget.value)}
          >
            <option value="">Add a component...</option>
            <For each={addable()}>{(c) => <option value={c.name}>{c.name}</option>}</For>
          </select>
          <Button
            size="sm"
            disabled={busy() || !picked()}
            onClick={() =>
              void run(async () => {
                await addMember(props.system, picked());
                setPicked("");
              })
            }
          >
            Add
          </Button>
        </div>
      </Show>

      <Show when={err()}>
        <p class="text-[11px] text-error">{err()}</p>
      </Show>
    </div>
  );
}
