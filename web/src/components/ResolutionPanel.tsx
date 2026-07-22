import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { componentSystemsKey, componentSystems, type Member } from "../lib/members";
import {
  bandLabel, byName, effectiveSecrets, effectiveSecretsKey, effectiveTags, effectiveTagsKey,
  effectiveVariables, effectiveVariablesKey, mergeResolved, type ValueKind,
} from "../lib/resolution";

// ResolutionPanel shows a component's effective tags as the cascade actually
// produces them: the winning value, the tier it came from, and everything it beat.
// The flat pill list elsewhere answers "what is it"; this answers "why is it
// that", which is the only question an operator has when a value looks wrong.
//
// The selector is the point of this panel existing. The system band is seeded
// from MEMBERSHIP, so a component serving two rooms resolves differently for
// each, and until now there was no way to see that anywhere in the console. It
// appears only above one membership: a component in a single system, which is
// nearly all of them, sees exactly what it saw before and pays nothing for the
// shared case.
// KindChip names which cascade a row came from. The kinds are shown rather than
// split into sections because their bands genuinely differ, and one list makes
// that visible: a reader sees a secret resolving from a location next to a
// variable resolving from a system and can ask why.
function KindChip(props: { kind: ValueKind }): JSX.Element {
  const tone = () =>
    props.kind === "secret" ? "border-warning/40 text-warning" :
    props.kind === "variable" ? "border-info/40 text-info" : "border-base-300 text-base-content/50";
  return (
    <span class={`shrink-0 rounded border px-1 text-[9.5px] uppercase tracking-wide ${tone()}`}>
      {props.kind}
    </span>
  );
}

export default function ResolutionPanel(props: { component: string }): JSX.Element {
  const [forSystem, setForSystem] = createSignal("");

  const memberships = useQuery(() => ({
    queryKey: componentSystemsKey(props.component),
    queryFn: () => componentSystems(props.component),
    refetchOnWindowFocus: false,
  }));
  const tags = useQuery(() => ({
    queryKey: effectiveTagsKey(props.component, forSystem()),
    queryFn: () => effectiveTags(props.component, forSystem()),
    refetchOnWindowFocus: false,
  }));
  // Variables and secrets do not take a system: the variable cascade seeds its
  // system band from the PRIMARY membership and the secret cascade has no system
  // band at all, so neither refetches when the selector moves.
  const variables = useQuery(() => ({
    queryKey: effectiveVariablesKey(props.component),
    queryFn: () => effectiveVariables(props.component),
    refetchOnWindowFocus: false,
  }));
  const secrets = useQuery(() => ({
    queryKey: effectiveSecretsKey(props.component),
    queryFn: () => effectiveSecrets(props.component),
    refetchOnWindowFocus: false,
  }));

  const systems = createMemo<Member[]>(() => memberships.data ?? []);
  const shared = createMemo(() => systems().length > 1);
  const primary = createMemo(() => systems().find((m) => m.primary)?.system ?? "");
  const groups = createMemo(() =>
    byName(mergeResolved(tags.data ?? [], variables.data ?? [], secrets.data ?? [])),
  );
  // The selector only governs the tag band, so it is offered only when a tag is
  // actually in play. Showing it above a list of variables would promise a
  // per-system answer the variable cascade does not give.
  const hasTags = createMemo(() => (tags.data ?? []).length > 0);

  return (
    <div class="flex flex-col gap-2">
      <div class="flex items-baseline justify-between gap-2">
        <span class="eyebrow">Effective values</span>
        <span class="shrink-0 text-[10.5px] text-base-content/40">what won, and what it beat</span>
      </div>
      <p class="text-[11px] text-base-content/50">
        A value here is not set on the component, it is the survivor of a cascade: platform, then location,
        then system, then the component itself, nearest wins. The three kinds do not share the same rungs:
        a variable takes its system band from the primary membership, and a secret has no system band at
        all, since a credential's owner is never the room a device happens to serve.
      </p>

      <Show when={shared() && hasTags()}>
        <div class="flex flex-col gap-1 rounded-box border border-warning/40 bg-warning/5 px-3 py-2">
          <span class="text-[11px] text-base-content/70">
            This component serves {systems().length} systems and resolves differently for each.
          </span>
          <div class="flex items-center gap-2">
            <select
              class="select select-bordered select-xs min-w-0 flex-1"
              aria-label="Resolve against"
              value={forSystem()}
              onChange={(e) => setForSystem(e.currentTarget.value)}
            >
              <option value="">{primary() ? `${primary()} (its default)` : "its default"}</option>
              <For each={systems()}>
                {(m) => (
                  <Show when={m.system !== primary()}>
                    <option value={m.system}>{m.system}</option>
                  </Show>
                )}
              </For>
            </select>
          </div>
        </div>
      </Show>

      <Show
        when={groups().length}
        fallback={
          <p class="rounded-box border border-dashed border-base-300 px-3 py-3 text-center text-[12px] text-base-content/45">
            No values reach this component.
          </p>
        }
      >
        <div class="divide-y divide-base-300 overflow-hidden rounded-box border border-base-300">
          <For each={groups()}>
            {(g) => (
              <div class="flex flex-col gap-1 px-3 py-2">
                <div class="flex items-baseline gap-2">
                  <KindChip kind={g.kind} />
                  <span class="min-w-0 flex-1 truncate font-mono text-[12px]">{g.name}</span>
                  {/* A secret carries no value here: its fields are masked and the
                      answer worth having is WHICH secret applies, not what is in
                      it. Reveal is audited and lives on the Secrets page. */}
                  <span class="font-mono text-[12px]">
                    {g.kind === "secret" ? <span class="text-base-content/35">masked</span> : (g.winner?.value ?? "—")}
                  </span>
                </div>
                <Show when={g.winner}>
                  <span class="text-[10.5px] text-base-content/45">
                    from {bandLabel(g.winner!.owner_kind)}
                    {g.winner!.owner_name ? ` ${g.winner!.owner_name}` : ""}
                  </span>
                </Show>
                {/* What it beat. Shown rather than hidden, because a value that
                    looks wrong is usually a value that won from the wrong tier. */}
                <Show when={g.shadowed.length}>
                  <div class="flex flex-col gap-0.5 border-l border-base-300 pl-2">
                    <For each={g.shadowed}>
                      {(s) => (
                        <span class="text-[10.5px] text-base-content/35 line-through decoration-base-content/25">
                          {s.kind === "secret" ? "" : `${s.value} `}from {bandLabel(s.owner_kind)}
                          {s.owner_name ? ` ${s.owner_name}` : ""}
                        </span>
                      )}
                    </For>
                  </div>
                </Show>
              </div>
            )}
          </For>
        </div>
      </Show>
    </div>
  );
}
