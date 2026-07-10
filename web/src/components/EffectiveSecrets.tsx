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

// tierLabel is the short, fixed-width badge text for a candidate's tier. The
// owner name is deliberately NOT in the badge (it is unbounded and would
// overflow the row); it renders as separate, truncatable text.
function tierLabel(r: ResolvedSecret): string {
  return r.owner_kind === "global" ? "Global" : r.owner_kind.charAt(0).toUpperCase() + r.owner_kind.slice(1);
}

// ownerText is the owning entity's name, or a word standing in for the global
// singleton (which has no owner row).
function ownerText(r: ResolvedSecret): string {
  return r.owner_kind === "global" ? "estate-wide" : r.owner_name || r.owner_kind;
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
        <span class="shrink-0 text-[10.5px] text-base-content/40">resolved down the scope cascade</span>
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
                class="flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-base-content/5"
                classList={{ "border-t border-base-300": i() > 0 }}
                onClick={() => setOpen(g)}
              >
                <span class="min-w-0 truncate font-data text-sm">{g.name}</span>
                <span class="badge badge-ghost badge-sm shrink-0">{g.winner.secret_type}</span>
                <span class="flex-1" />
                <span class="hidden max-w-[10rem] truncate text-xs text-base-content/50 sm:inline">{ownerText(g.winner)}</span>
                <span class="badge badge-primary badge-sm shrink-0">{tierLabel(g.winner)}</span>
                <span class="shrink-0 text-base-content/40"><ChevronRight size={14} /></span>
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
  const w = () => props.group.winner;
  return (
    <div class="flex flex-col gap-5">
      <div class="flex flex-col gap-2">
        <div class="flex items-center gap-2">
          <span class="eyebrow">Resolved value</span>
          <span class="badge badge-ghost badge-sm shrink-0">{w().secret_type}</span>
        </div>
        <div class="flex items-center gap-2 text-sm">
          <span class="badge badge-primary badge-sm shrink-0">{tierLabel(w())}</span>
          <span class="min-w-0 truncate text-base-content/70">{ownerText(w())}</span>
        </div>
        <SecretFields secretId={w().id} fields={w().fields} canReveal={props.canReveal} />
      </div>

      <div class="flex flex-col gap-1.5">
        <span class="eyebrow">Cascade</span>
        <p class="text-[11px] text-base-content/40">most-specific wins: component &rsaquo; system &rsaquo; location &rsaquo; global</p>
        <div class="overflow-hidden rounded-box border border-base-300">
          <For each={[w(), ...props.group.shadowed]}>
            {(r, i) => (
              <div class="flex items-center gap-2 px-3 py-2" classList={{ "border-t border-base-300": i() > 0 }}>
                <span class="badge badge-sm shrink-0" classList={{ "badge-primary": r.winner, "badge-ghost": !r.winner }}>{tierLabel(r)}</span>
                <span class="min-w-0 flex-1 truncate text-sm" classList={{ "text-base-content/40": !r.winner }}>{ownerText(r)}</span>
                <Show when={r.winner}><span class="shrink-0 text-primary"><Check size={14} /></span></Show>
              </div>
            )}
          </For>
        </div>
      </div>
    </div>
  );
}
