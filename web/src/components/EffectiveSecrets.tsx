import { For, Show, createMemo, type JSX } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { effectiveSecrets, effectiveSecretsKey, type ResolvedSecret } from "../lib/secrets";
import { describeError } from "../lib/format";

// EffectiveSecrets renders a component's secret cascade: the values that resolve
// onto it from every tier, most-specific winning. It is a learning surface as
// much as an operator one, so it shows not just the winner but the candidates it
// shadowed, and names the tier each comes from. Values are masked (a secret
// field is never revealed here).

// bandLabel names the owner tier a candidate comes from: the bare tier for a
// global secret, "Tier: owner" for a scoped one.
function bandLabel(r: ResolvedSecret): string {
  if (r.owner_kind === "global") return "Global";
  const tier = r.owner_kind.charAt(0).toUpperCase() + r.owner_kind.slice(1);
  return r.owner_name ? `${tier}: ${r.owner_name}` : tier;
}

function fieldsInline(r: ResolvedSecret): JSX.Element {
  return (
    <span class="font-data text-xs text-base-content/70">
      <For each={r.fields}>
        {(f, i) => (
          <>
            <Show when={i()}><span class="text-base-content/30">, </span></Show>
            <span class="text-base-content/50">{f.name}</span>=<span classList={{ "text-base-content/40": f.secret }}>{f.value}</span>
          </>
        )}
      </For>
      <Show when={!r.fields.length}><span class="text-base-content/40">no fields</span></Show>
    </span>
  );
}

export default function EffectiveSecrets(props: { component: string }): JSX.Element {
  const q = useQuery(() => ({
    queryKey: effectiveSecretsKey(props.component),
    queryFn: () => effectiveSecrets(props.component),
  }));

  // Group the flat candidate list by secret name, preserving the API's order
  // (winner first, then shadowed by descending specificity). The winner carries
  // the resolved value; the rest are what it overrode.
  const groups = createMemo(() => {
    const by = new Map<string, ResolvedSecret[]>();
    for (const r of q.data ?? []) {
      const g = by.get(r.name);
      if (g) g.push(r);
      else by.set(r.name, [r]);
    }
    return [...by.entries()].map(([name, rows]) => ({
      name,
      winner: rows.find((r) => r.winner) ?? rows[0],
      shadowed: rows.filter((r) => !r.winner),
    }));
  });

  return (
    <div class="flex flex-col gap-2">
      <div class="flex items-baseline justify-between gap-2">
        <span class="eyebrow">Effective secrets</span>
        <span class="text-[10.5px] text-base-content/40">most-specific wins: component › system › location › global</span>
      </div>
      <Show when={q.error}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{describeError(q.error)}</span></div>
      </Show>
      <Show when={!q.isLoading && !q.error && !groups().length}>
        <p class="text-sm text-base-content/50">No secrets resolve onto this component.</p>
      </Show>
      <Show when={groups().length}>
        <div class="flex flex-col gap-2">
          <For each={groups()}>
            {(g) => (
              <div class="rounded-box border border-base-300 p-3">
                <div class="flex flex-wrap items-center gap-2">
                  <span class="font-data text-sm">{g.name}</span>
                  <span class="badge badge-soft badge-neutral badge-sm text-[10px]">{g.winner.secret_type}</span>
                  <span class="flex-1" />
                  <span class="badge badge-primary badge-sm text-[10px]">{bandLabel(g.winner)}</span>
                </div>
                <div class="mt-1.5">{fieldsInline(g.winner)}</div>
                <Show when={g.shadowed.length}>
                  <div class="mt-2 border-t border-base-300 pt-2">
                    <span class="text-[10px] uppercase tracking-wide text-base-content/40">Shadowed</span>
                    <div class="mt-1 flex flex-col gap-1">
                      <For each={g.shadowed}>
                        {(s) => (
                          <div class="flex items-center gap-2 text-xs text-base-content/50">
                            <span class="badge badge-ghost badge-sm text-[10px]">{bandLabel(s)}</span>
                            <span class="line-through decoration-base-content/20">{fieldsInline(s)}</span>
                          </div>
                        )}
                      </For>
                    </div>
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
