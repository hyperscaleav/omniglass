import { For, Show, createMemo, type JSX } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import HealthBadge from "./HealthBadge";
import HealthHistory from "./HealthHistory";
import { ChevronRight } from "./icons";
import { describeError } from "../lib/format";
import {
  causes,
  chainSentence,
  holdingRoles,
  impactPhrase,
  impairedRoles,
  locationHealth,
  locationHealthKey,
  quorumLabel,
  roles as rolesOf,
  systemHealth,
  systemHealthKey,
  systems as systemsOf,
  transitions as transitionsOf,
  type Cause,
  type HealthRole,
} from "../lib/health";

// The reconciliation panel: the point of the health slice, and the one surface that
// has to answer "why is this degraded" without sending the operator anywhere else.
//
// A verdict badge alone is what every monitoring tool already ships, and it is
// exactly what nobody trusts, because it asserts a conclusion and hides the
// reasoning. This panel names the whole chain instead, in the order it actually
// runs:
//
//   an ALARM on a component  ->  the CAPABILITY it degrades  ->  the ROLE that
//   falls below quorum  ->  what that role CONTRIBUTES  ->  this verdict
//
// Every link is a real API field, joined here and nowhere else: the role's
// `degraded` list is the capabilities an alarm took away, and the role's `alarms`
// are the alarms that took them. The verdict itself is never recomputed in the
// browser; the server sends it and the panel shows its derivation, so the console
// and the API can never disagree about whether a room is out.

const SEVERITY: Record<string, string> = { critical: "badge-error", warning: "badge-warning", info: "badge-info" };
const severityBadge = (s: string) => SEVERITY[s] ?? "badge-ghost";

const IMPACT: Record<string, string> = { outage: "badge-error", degraded: "badge-warning" };
const impactBadge = (i: string) => IMPACT[i] ?? "badge-ghost";

// One labelled link of the chain. The caption is what makes the row teach: the
// operator learns the vocabulary (capability, quorum, impact) by reading their own
// outage in it.
function Step(props: { caption: string; children: JSX.Element }) {
  return (
    <div class="flex min-w-0 flex-1 basis-40 flex-col gap-1">
      <span class="text-[9.5px] font-semibold uppercase tracking-wider text-base-content/40">{props.caption}</span>
      <div class="flex min-w-0 flex-col gap-1">{props.children}</div>
    </div>
  );
}

function Arrow() {
  return (
    <span class="hidden shrink-0 self-center text-base-content/25 sm:inline-flex" aria-hidden="true">
      <ChevronRight size={16} />
    </span>
  );
}

// ChainRow draws one full causal chain: the alarms that took a single required
// capability away, that capability, the role it pushed below quorum, and what the
// role contributes to the system.
function ChainRow(props: { cause: Cause; role: HealthRole; onOpenComponent?: (name: string) => void }) {
  const r = () => props.role;
  return (
    <div class="flex flex-wrap items-stretch gap-2 rounded-box bg-base-200/60 px-3 py-2.5">
      <Step caption="Alarm on a component">
        <Show
          when={props.cause.alarms.length}
          fallback={<span class="text-[11.5px] italic text-base-content/45">no alarm names this capability</span>}
        >
          <For each={props.cause.alarms}>
            {(a) => (
              <div class="flex min-w-0 flex-col gap-0.5">
                <div class="flex flex-wrap items-center gap-1.5">
                  <span class={`badge badge-soft badge-xs ${severityBadge(a.severity)}`}>{a.severity}</span>
                  <Show
                    when={props.onOpenComponent}
                    fallback={<span class="font-data text-[11.5px] text-base-content/70">{a.component}</span>}
                  >
                    <button
                      type="button"
                      class="link font-data text-[11.5px] text-base-content/70"
                      onClick={() => props.onOpenComponent!(a.component)}
                    >
                      {a.component}
                    </button>
                  </Show>
                </div>
                <span class="text-[12px] text-base-content/80">{a.message}</span>
              </div>
            )}
          </For>
        </Show>
      </Step>
      <Arrow />
      <Step caption="Capability degraded">
        <span class="badge badge-error badge-soft badge-sm w-fit font-data">{props.cause.capability}</span>
        <span class="text-[11px] text-base-content/45">the role requires it, this component no longer provides it</span>
      </Step>
      <Arrow />
      <Step caption="Role below quorum">
        <span class="text-[12.5px] font-medium">{r().display_name || r().name}</span>
        <span class="tnum text-[11.5px] text-base-content/60">{quorumLabel(r())}</span>
      </Step>
      <Arrow />
      <Step caption="Contributes">
        <span class={`badge badge-soft badge-sm w-fit ${impactBadge(r().impact)}`}>{impactPhrase(r().impact)}</span>
        <span class="text-[11px] text-base-content/45">the system takes the worst of these</span>
      </Step>
    </div>
  );
}

// ImpairedRole is one block of the reconciliation: the role, the one-line claim,
// then a chain per capability an alarm took away, then what the role requires and
// who fills it.
function ImpairedRole(props: { role: HealthRole; verdict: string; onOpenComponent?: (name: string) => void }) {
  const r = () => props.role;
  const chains = createMemo(() => causes(r()));
  const degraded = () => new Set(r().degraded ?? []);
  return (
    <div class="flex flex-col gap-2 px-3 py-3">
      <div class="flex flex-wrap items-baseline gap-2">
        <span class="text-sm font-medium">{r().display_name || r().name}</span>
        <span class="font-data text-[11px] text-base-content/45">{r().name}</span>
        <span class="flex-1" />
        <span class="badge badge-warning badge-soft badge-sm">impaired</span>
        <span class={`badge badge-soft badge-sm ${impactBadge(r().impact)}`}>{impactPhrase(r().impact)}</span>
        <span class="tnum shrink-0 text-[11.5px] text-base-content/60">{quorumLabel(r())}</span>
      </div>

      {/* The claim, in one sentence, so the chain below is read as an expansion of
          something already stated rather than a puzzle to assemble. */}
      <p class="text-[12.5px] text-base-content/75">{chainSentence(r(), props.verdict)}</p>

      <Show
        when={chains().length}
        fallback={
          <div class="rounded-box bg-base-200/60 px-3 py-2.5 text-[12px] text-base-content/60">
            Nothing is broken here. Every component assigned to this role can still fill it; there are simply fewer of
            them than the quorum wants, so assigning one more clears it.
          </div>
        }
      >
        <For each={chains()}>{(c) => <ChainRow cause={c} role={r()} onOpenComponent={props.onOpenComponent} />}</For>
      </Show>

      <div class="flex flex-wrap items-center gap-1.5">
        <span class="text-[10.5px] uppercase tracking-wide text-base-content/40">requires</span>
        <Show
          when={(r().required ?? []).length}
          fallback={<span class="text-[11px] italic text-base-content/40">nothing: any component can fill it</span>}
        >
          <For each={r().required ?? []}>
            {(c) => (
              <span
                class={`badge badge-sm font-data ${degraded().has(c) ? "badge-error badge-soft" : "badge-ghost"}`}
                title={degraded().has(c) ? "An active alarm has taken this away" : "Still provided"}
              >
                {c}
                <Show when={degraded().has(c)}> degraded</Show>
              </span>
            )}
          </For>
        </Show>
      </div>

      <div class="flex flex-wrap items-center gap-1.5">
        <span class="text-[10.5px] uppercase tracking-wide text-base-content/40">assigned</span>
        <Show
          when={(r().assigned_to ?? []).length}
          fallback={<span class="text-[11px] italic text-base-content/40">nobody yet</span>}
        >
          <For each={r().assigned_to ?? []}>
            {(c) => <span class="badge badge-outline badge-sm font-data">{c}</span>}
          </For>
        </Show>
      </div>
    </div>
  );
}

// SystemHealthPanel: the system's verdict, why it holds, and when it last changed.
export default function SystemHealthPanel(props: { system: string; onOpenComponent?: (name: string) => void }) {
  const q = useQuery(() => ({
    queryKey: systemHealthKey(props.system),
    queryFn: () => systemHealth(props.system),
    staleTime: 30_000,
    refetchOnWindowFocus: false,
  }));
  const verdict = () => q.data?.verdict ?? "";
  const impaired = createMemo(() => impairedRoles(q.data));
  const holding = createMemo(() => holdingRoles(q.data));
  const all = createMemo(() => rolesOf(q.data));

  return (
    <div class="flex flex-col gap-2">
      <div class="flex flex-wrap items-center gap-2">
        <span class="eyebrow">Health</span>
        <HealthBadge verdict={verdict()} />
        <span class="flex-1" />
        <span class="shrink-0 text-[10.5px] text-base-content/40">why this system reads the way it does</span>
      </div>

      <Show when={q.error}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{describeError(q.error)}</span></div>
      </Show>
      <Show when={q.isLoading}>
        <p class="text-sm text-base-content/50">Reading health…</p>
      </Show>

      <Show when={q.data}>
        {/* The model, stated once, so the blocks below are read as its instances. */}
        <p class="text-[11px] text-base-content/50">
          An alarm on a component degrades a capability. A role that requires that capability can no longer be filled by
          that component, and drops below its quorum. The system takes the worst impact among its impaired roles.
        </p>

        <Show
          when={all().length}
          fallback={
            <p class="rounded-box border border-dashed border-base-300 px-3 py-4 text-center text-[12px] text-base-content/45">
              This system declares no roles, so there is nothing to reconcile. Give it a role (a slot it needs filled)
              and its health starts meaning something.
            </p>
          }
        >
          <Show
            when={impaired().length}
            fallback={
              <div role="status" class="rounded-box border border-success/40 bg-success/10 px-3 py-3 text-[12.5px] text-base-content/80">
                This system is healthy. All {all().length} role{all().length === 1 ? "" : "s"} it needs are filled and
                meet their quorum, so no role contributes anything worse. Nothing is impaired.
              </div>
            }
          >
            <div class="flex flex-col gap-1">
              <span class="text-[11px] text-base-content/50">
                {impaired().length} of {all().length} role{all().length === 1 ? "" : "s"} impaired, worst first.
              </span>
              <div class="divide-y divide-base-300 overflow-hidden rounded-box border border-base-300">
                <For each={impaired()}>
                  {(r) => <ImpairedRole role={r} verdict={verdict()} onOpenComponent={props.onOpenComponent} />}
                </For>
              </div>
            </div>
          </Show>

          {/* What is holding, named rather than implied: it is the other half of why
              a system is only degraded and not out. */}
          <Show when={impaired().length && holding().length}>
            <div class="flex flex-wrap items-center gap-1.5">
              <span class="text-[10.5px] uppercase tracking-wide text-base-content/40">holding</span>
              <For each={holding()}>
                {(r) => (
                  <span class="badge badge-ghost badge-sm gap-1" title={quorumLabel(r)}>
                    {r.display_name || r.name}
                    <span class="tnum text-[10px] text-base-content/50">{r.satisfying}/{r.quorum}</span>
                  </span>
                )}
              </For>
            </div>
          </Show>
        </Show>

        <HealthHistory transitions={transitionsOf(q.data)} verdict={verdict()} />
      </Show>
    </div>
  );
}

// LocationHealthPanel: worst wins over the systems placed beneath. A location has
// no roles of its own, so the drill-down is the systems themselves, each carrying
// the verdict whose reconciliation lives on its own detail.
export function LocationHealthPanel(props: { location: string; onOpenSystem?: (name: string) => void }) {
  const q = useQuery(() => ({
    queryKey: locationHealthKey(props.location),
    queryFn: () => locationHealth(props.location),
    staleTime: 30_000,
    refetchOnWindowFocus: false,
  }));
  const verdict = () => q.data?.verdict ?? "";
  const beneath = createMemo(() => systemsOf(q.data));
  const worst = createMemo(() => beneath().find((s) => s.verdict === verdict()));

  return (
    <div class="flex flex-col gap-2">
      <div class="flex flex-wrap items-center gap-2">
        <span class="eyebrow">Health</span>
        <HealthBadge verdict={verdict()} />
        <span class="flex-1" />
        <span class="shrink-0 text-[10.5px] text-base-content/40">worst wins over the systems beneath</span>
      </div>

      <Show when={q.error}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{describeError(q.error)}</span></div>
      </Show>
      <Show when={q.isLoading}>
        <p class="text-sm text-base-content/50">Reading health…</p>
      </Show>

      <Show when={q.data}>
        <p class="text-[11px] text-base-content/50">
          A location has no roles of its own. It takes the worst verdict among every system placed anywhere beneath it,
          and each of those systems can say why on its own detail.
        </p>

        <Show
          when={beneath().length}
          fallback={
            <p class="rounded-box border border-dashed border-base-300 px-3 py-4 text-center text-[12px] text-base-content/45">
              No system is placed beneath this location, so there is nothing to roll up.
            </p>
          }
        >
          <Show when={worst()}>
            {(w) => (
              <p class="text-[12.5px] text-base-content/75">
                Worst of {beneath().length} system{beneath().length === 1 ? "" : "s"} beneath: {w().name} reads{" "}
                {w().verdict}, so this location reads {verdict()}.
              </p>
            )}
          </Show>
          <div class="divide-y divide-base-300 overflow-hidden rounded-box border border-base-300">
            <For each={beneath()}>
              {(s) => (
                <Show
                  when={props.onOpenSystem}
                  fallback={
                    <div class="flex items-center gap-2.5 px-3 py-2">
                      <span class="min-w-0 flex-1 truncate font-data text-sm">{s.name}</span>
                      <HealthBadge verdict={s.verdict} />
                    </div>
                  }
                >
                  <button
                    type="button"
                    class="flex w-full items-center gap-2.5 px-3 py-2 text-left hover:bg-base-content/5"
                    onClick={() => props.onOpenSystem!(s.name)}
                  >
                    <span class="min-w-0 flex-1 truncate font-data text-sm">{s.name}</span>
                    <HealthBadge verdict={s.verdict} />
                    <ChevronRight size={14} />
                  </button>
                </Show>
              )}
            </For>
          </div>
        </Show>

        <HealthHistory transitions={transitionsOf(q.data)} verdict={verdict()} />
      </Show>
    </div>
  );
}
