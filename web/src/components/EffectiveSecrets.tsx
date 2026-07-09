import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import Drawer from "./Drawer";
import SecretFields from "./SecretFields";
import { effectiveSecrets, effectiveSecretsKey, type ResolvedSecret } from "../lib/secrets";
import { describeError } from "../lib/format";
import { ChevronRight, Check } from "./icons";

// EffectiveSecrets lists the secrets that resolve onto a component down the
// cascade, one row per name (the winner). Opening a row drops a blade that
// reveals the resolved value (audited decrypt + copy) and shows the full
// hierarchy of how it won: the winning tier and the candidates it shadowed.

// bandLabel names the owner tier a candidate comes from: the bare tier for a
// global secret, "Tier: owner" for a scoped one.
function bandLabel(r: ResolvedSecret): string {
  if (r.owner_kind === "global") return "Global";
  const tier = r.owner_kind.charAt(0).toUpperCase() + r.owner_kind.slice(1);
  return r.owner_name ? `${tier}: ${r.owner_name}` : tier;
}

function fieldsInline(r: ResolvedSecret): JSX.Element {
  return (
    <span class="font-data text-xs">
      <For each={r.fields}>
        {(f, i) => (
          <>
            <Show when={i()}><span class="text-base-content/30">, </span></Show>
            <span class="text-base-content/45">{f.name}</span>=<span>{f.value}</span>
          </>
        )}
      </For>
      <Show when={!r.fields.length}><span class="text-base-content/40">no fields</span></Show>
    </span>
  );
}

type Group = { name: string; winner: ResolvedSecret; shadowed: ResolvedSecret[] };

export default function EffectiveSecrets(props: { component: string; canReveal: boolean }): JSX.Element {
  const q = useQuery(() => ({
    queryKey: effectiveSecretsKey(props.component),
    queryFn: () => effectiveSecrets(props.component),
  }));

  // Group the flat candidate list by secret name, preserving the API's order
  // (winner first, then shadowed by descending specificity).
  const groups = createMemo<Group[]>(() => {
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

  const [open, setOpen] = createSignal<Group | null>(null);

  return (
    <div class="flex flex-col gap-2">
      <div class="flex items-baseline justify-between gap-2">
        <span class="eyebrow">Effective secrets</span>
        <span class="text-[10.5px] text-base-content/40">resolved down the scope cascade</span>
      </div>
      <Show when={q.error}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{describeError(q.error)}</span></div>
      </Show>
      <Show when={!q.isLoading && !q.error && !groups().length}>
        <p class="text-sm text-base-content/50">No secrets resolve onto this component.</p>
      </Show>
      <Show when={groups().length}>
        <div class="overflow-hidden rounded-box border border-base-300">
          <For each={groups()}>
            {(g, i) => (
              <button
                class="flex w-full items-center gap-2.5 px-3 py-2 text-left hover:bg-base-content/5"
                classList={{ "border-t border-base-300": i() > 0 }}
                onClick={() => setOpen(g)}
              >
                <span class="font-data text-sm">{g.name}</span>
                <span class="badge badge-ghost badge-sm">{g.winner.secret_type}</span>
                <span class="flex-1" />
                <span class="badge badge-primary badge-sm">{bandLabel(g.winner)}</span>
                <ChevronRight size={14} />
              </button>
            )}
          </For>
        </div>
      </Show>

      <Drawer open={!!open()} onClose={() => setOpen(null)} title={<span class="font-data">{open()?.name}</span>}>
        <Show when={open()}>
          {(g) => <CascadeDetail group={g()} canReveal={props.canReveal} />}
        </Show>
      </Drawer>
    </div>
  );
}

// CascadeDetail is the blade body: the resolved (revealable) value on top, then
// the hierarchy of how it won.
function CascadeDetail(props: { group: Group; canReveal: boolean }): JSX.Element {
  return (
    <div class="flex flex-col gap-5">
      <div class="flex flex-col gap-2">
        <div class="flex items-center gap-2">
          <span class="eyebrow">Resolved value</span>
          <span class="badge badge-ghost badge-sm">{props.group.winner.secret_type}</span>
          <span class="flex-1" />
          <span class="badge badge-primary badge-sm">{bandLabel(props.group.winner)}</span>
        </div>
        <SecretFields secretId={props.group.winner.id} fields={props.group.winner.fields} canReveal={props.canReveal} />
      </div>

      <div class="flex flex-col gap-1.5">
        <span class="eyebrow">Cascade</span>
        <p class="text-[11px] text-base-content/40">most-specific wins: component › system › location › global</p>
        <div class="overflow-hidden rounded-box border border-base-300">
          <For each={[props.group.winner, ...props.group.shadowed]}>
            {(r, i) => (
              <div class="flex items-center gap-2 px-3 py-2" classList={{ "border-t border-base-300": i() > 0 }}>
                <span class="badge badge-sm" classList={{ "badge-primary": r.winner, "badge-ghost": !r.winner }}>{bandLabel(r)}</span>
                <span class="flex-1" />
                <span classList={{ "line-through decoration-base-content/20 text-base-content/40": !r.winner, "text-base-content/70": r.winner }}>{fieldsInline(r)}</span>
                <Show when={r.winner}><Check size={13} /></Show>
              </div>
            )}
          </For>
        </div>
      </div>
    </div>
  );
}
