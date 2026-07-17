import { For, Show, createMemo, type JSX } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { effectiveVariables, effectiveVariablesKey, displayValue, type ResolvedVariable } from "../lib/variables";
import { ValueDisplay } from "../pages/Variables";
import { type BladeDef } from "../lib/blades";
import { describeError } from "../lib/format";
import KVRow from "./KVRow";
import { Check } from "./icons";

// EffectiveVariables lists the variables that resolve onto a component down the
// cascade, one row per name (the winner). A row opens the variable's cascade as a
// nested blade on the component page's shared stack (kind "variable-cascade", via
// ctx.openBlade) rather than a separate overlay, which a higher-z blade would
// hide. The blade shows the resolved value and the full hierarchy of how it won.

// The blade id encodes the component and variable name, so the blade body can
// re-resolve the cascade from the id alone (blades carry only { kind, id }).
export const varCascadeBladeId = (component: string, variable: string): string => `${component} ${variable}`;
const splitCascadeId = (id: string): [string, string] => {
  const i = id.indexOf(" ");
  return i < 0 ? [id, ""] : [id.slice(0, i), id.slice(i + 1)];
};

function tierLabel(r: ResolvedVariable): string {
  return r.owner_kind === "global" ? "Global" : r.owner_kind.charAt(0).toUpperCase() + r.owner_kind.slice(1);
}

function ownerText(r: ResolvedVariable): string {
  return r.owner_kind === "global" ? "estate-wide" : r.owner_name || r.owner_kind;
}

type Group = { name: string; winner: ResolvedVariable; shadowed: ResolvedVariable[] };

function groupsOf(rows: ResolvedVariable[]): Group[] {
  const by = new Map<string, ResolvedVariable[]>();
  for (const r of rows) {
    const g = by.get(r.name);
    if (g) g.push(r);
    else by.set(r.name, [r]);
  }
  return [...by.entries()].map(([name, rs]) => ({
    name,
    winner: rs.find((r) => r.winner) ?? rs[0],
    shadowed: rs.filter((r) => !r.winner),
  }));
}

export default function EffectiveVariables(props: { component: string; onOpen: (variableName: string) => void }): JSX.Element {
  const q = useQuery(() => ({
    queryKey: effectiveVariablesKey(props.component),
    queryFn: () => effectiveVariables(props.component),
  }));
  const groups = createMemo(() => groupsOf(q.data ?? []));

  return (
    <div class="flex flex-col gap-2">
      <div class="flex items-baseline justify-between gap-2">
        <span class="eyebrow">Effective variables</span>
        <span class="shrink-0 text-[10.5px] text-base-content/40">resolved down the scope cascade</span>
      </div>
      <Show when={q.error}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{describeError(q.error)}</span></div>
      </Show>
      <Show when={!q.isLoading && !q.error && !groups().length}>
        <p class="text-sm text-base-content/50">No variables resolve onto this component.</p>
      </Show>
      <Show when={groups().length}>
        <div class="overflow-hidden rounded-box border border-base-300">
          <For each={groups()}>
            {(g, i) => (
              <KVRow
                first={i() === 0}
                label={g.name}
                labelMono
                typeBadge={g.winner.value_type}
                value={displayValue(g.winner.value)}
                origin={tierLabel(g.winner)}
                onDrillIn={() => props.onOpen(g.name)}
              />
            )}
          </For>
        </div>
      </Show>
    </div>
  );
}

// variableCascadeBlade renders one variable's cascade on the shared blade stack. It
// re-resolves the effective variables for the component encoded in the id and
// picks out the named variable's group, so it renders from the id alone across a
// refetch (the shared-stack contract).
export const variableCascadeBlade: BladeDef = {
  Title: (p) => <span class="font-data">{splitCascadeId(p.id)[1]}</span>,
  Body: (p) => <VariableCascadeBody id={p.id} />,
};

function VariableCascadeBody(p: { id: string }): JSX.Element {
  const parts = createMemo(() => splitCascadeId(p.id));
  const q = useQuery(() => ({
    queryKey: effectiveVariablesKey(parts()[0]),
    queryFn: () => effectiveVariables(parts()[0]),
  }));
  const group = createMemo<Group | undefined>(() => groupsOf((q.data ?? []).filter((r) => r.name === parts()[1]))[0]);

  return (
    <Show when={group()} fallback={<p class="text-sm text-base-content/50">This variable no longer resolves onto the component.</p>}>
      {(g) => <CascadeDetail group={g()} />}
    </Show>
  );
}

// CascadeDetail is the blade content: the resolved value on top, then the
// hierarchy of how it won.
function CascadeDetail(props: { group: Group }): JSX.Element {
  const w = () => props.group.winner;
  return (
    <div class="flex flex-col gap-5">
      <div class="flex flex-col gap-2">
        <div class="flex items-center gap-2">
          <span class="eyebrow">Resolved value</span>
          <span class="badge badge-ghost badge-sm shrink-0">{w().value_type}</span>
        </div>
        <div class="flex items-center gap-2 text-sm">
          <span class="badge badge-primary badge-sm shrink-0">{tierLabel(w())}</span>
          <span class="min-w-0 truncate text-base-content/70">{ownerText(w())}</span>
        </div>
        <ValueDisplay valueType={w().value_type} value={w().value} />
      </div>

      <div class="flex flex-col gap-1.5">
        <span class="eyebrow">Cascade</span>
        <p class="text-[11px] text-base-content/40">falls global &rsaquo; location &rsaquo; system &rsaquo; component; the deepest wins</p>
        <div class="overflow-hidden rounded-box border border-base-300">
          <For each={[w(), ...props.group.shadowed].slice().sort((a, b) => a.band - b.band || b.depth - a.depth)}>
            {(r, i) => (
              <div class="flex items-center gap-2 px-3 py-2" classList={{ "border-t border-base-300": i() > 0 }}>
                <span class="badge badge-sm shrink-0" classList={{ "badge-primary": r.winner, "badge-ghost": !r.winner }}>{tierLabel(r)}</span>
                <span class="min-w-0 flex-1 truncate text-sm" classList={{ "text-base-content/40": !r.winner }}>{ownerText(r)}</span>
                <span class="hidden max-w-32 truncate font-data text-xs text-base-content/50 sm:inline">{displayValue(r.value)}</span>
                <Show when={r.winner}><span class="shrink-0 text-primary"><Check size={14} /></span></Show>
              </div>
            )}
          </For>
        </div>
      </div>
    </div>
  );
}
