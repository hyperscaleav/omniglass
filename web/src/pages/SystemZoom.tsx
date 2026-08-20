import { For, Show, createEffect, createMemo } from "solid-js";
import { useNavigate, useParams } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import Breadcrumb from "../components/Breadcrumb";
import HealthBadge from "../components/HealthBadge";
import HealthHistory from "../components/HealthHistory";
import FleetShell from "../components/FleetShell";
import { fleetTiles } from "../lib/fleet_tiles";
import { createSignal } from "solid-js";
import type { Chip } from "../lib/predicate";
import { FLEET_VIEW_KEY, ancestors, fleetView, locationIndex } from "../lib/fleet";
import { systemHealth, systemHealthKey } from "../lib/health";
import { systemRoles, systemRolesKey } from "../lib/system_roles";
import { alarmRows, sinceOf, systemZoomVM, type SlotVM } from "../lib/system_zoom";
import { slotStrip } from "../lib/slot_strip";
import { SYSTEMS_KEY, listSystems } from "../lib/systems";
import { entityLabel } from "../lib/entities";
import { describeError, fmtTime } from "../lib/format";
import { durationText } from "../lib/timeline";
import { severityRank } from "../lib/alarms";

// The system zoom (#636): the typed slots a system needs filled, at the
// identity route behind ?zoom=1 (ADR-0126). One card per role with the
// server's own arithmetic, roles grouped by choice with the active alternate
// marked and the losing build rendered as the legal alternative it is, the
// shared occupants chipped with the other system they serve, and the members
// filling no role in their own strip, which is a state and not an error.
export default function SystemZoom() {
  const params = useParams<{ id: string }>();
  const navigate = useNavigate();
  const id = () => params.id;
  // Pinned at setup, like HealthHistory's: a "now" that moved under the
  // since-line would re-age it on every unrelated re-render.
  const pageNow = Date.now();

  const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const health = useQuery(() => ({ queryKey: systemHealthKey(id()), queryFn: () => systemHealth(id()) }));
  const declared = useQuery(() => ({ queryKey: systemRolesKey(id()), queryFn: () => systemRoles(id()) }));

  const system = createMemo(() => (view.data?.systems ?? []).find((s) => s.id === id()));

  // A name-shaped address resolves to the uuid, query kept (#759's rule; the
  // zoom branch runs before the inventory detail's own fallback could).
  createEffect(() => {
    if (!view.data || system()) return;
    const matches = (view.data.systems ?? []).filter((s) => s.name === id());
    if (matches.length === 1) navigate(`/systems/${matches[0].id}?zoom=1`, { replace: true });
  });

  const strip = createMemo(() => (health.data ? slotStrip(health.data) : undefined));
  const systemsList = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems }));
  const standard = createMemo(() => (systemsList.data ?? []).find((x) => x.id === id())?.standard);

  const vm = createMemo(() => {
    if (!view.data || !health.data || !declared.data) return undefined;
    return systemZoomVM(health.data, declared.data, view.data, id());
  });

  const alarms = createMemo(() => (view.data && health.data ? alarmRows(health.data, view.data, id()) : []));
  const tiles = createMemo(() => (view.data ? fleetTiles(view.data) : undefined));
  const [chips, setChips] = createSignal<Chip[]>([]);

  const crumbs = createMemo(() => {
    if (!view.data) return [];
    const chain = system()?.location ? ancestors(system()!.location!, locationIndex(view.data)) : [];
    return [
      { key: "fleet", label: "Fleet", onClick: () => navigate("/fleet") },
      ...chain.map((l) => ({
        key: l.id,
        label: entityLabel(l),
        onClick: () => navigate(`/locations/${l.id}?zoom=1`),
      })),
    ];
  });

  const pending = () => view.isPending || health.isPending || declared.isPending;
  const failed = () => view.isError || health.isError || declared.isError;

  return (
    <Page
      title={system() ? entityLabel(system()!) : "System"}
      breadcrumb={<Breadcrumb crumbs={crumbs()} />}
    >
      <Show when={!pending()} fallback={<div class="skeleton h-32 w-full" />}>
        <Show
          when={!failed()}
          fallback={
            <div role="alert" class="alert alert-error alert-soft text-sm">
              {describeError(view.error ?? health.error ?? declared.error)}
            </div>
          }
        >
          <FleetShell
            storageKey="fleet"
            tiles={tiles()}
            rows={[]}
            filterKeys={[]}
            chips={chips}
            onChips={setChips}
            header={
              <div data-testid="system-header" class="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
                <HealthBadge verdict={health.data?.verdict} size="sm" />
                <Show when={health.data && sinceOf(health.data, pageNow)}>
                  {(sc) => <span data-testid="since-line" class="tabular-nums text-base-content/70">since {fmtTime(sc().ts)} · {durationText(sc().ms)}</span>}
                </Show>
                {/* Slot arithmetic leads only when something is missing: a
                    deployed room fills every role, so a full house says
                    nothing about slots (#785). */}
                <Show when={strip() && strip()!.filled < strip()!.total}>
                  {(_) => <span class="tabular-nums text-base-content/80">{strip()!.filled} of {strip()!.total} slots filled</span>}
                </Show>
                <Show when={standard()}>
                  <span class="text-base-content/30">·</span>
                  <span class="font-mono text-xs text-base-content/60">{standard()}</span>
                </Show>
                <Show when={system()?.location}>
                  {(loc) => (
                    <>
                      <span class="text-base-content/30">·</span>
                      <button type="button" class="cursor-pointer text-xs text-primary hover:underline" onClick={() => navigate(`/locations/${loc()}?zoom=1`)}>Open location</button>
                    </>
                  )}
                </Show>
              </div>
            }
          >
          <Show when={vm()}>
            {(z) => (
              <div class="flex min-w-0 flex-1 flex-col">
                <div class="flex flex-col gap-5 p-4">
                  {/* Cause before arithmetic (#785): what is wrong, on which
                      component, impairing which role, since when. */}
                  <Show when={alarms().length > 0}>
                    <section data-testid="alarm-strip" class="flex flex-col gap-1.5 rounded-box border p-3" classList={{ "border-error/40 bg-error/5": severityRank(alarms()[0].severity) === 0, "border-warning/40 bg-warning/5": severityRank(alarms()[0].severity) !== 0 }}>
                      <h2 class="eyebrow">Active alarms</h2>
                      <For each={alarms()}>
                        {(a) => (
                          <div class="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 text-sm">
                            <span class="badge badge-sm" classList={{ "badge-error badge-soft": a.severity === "critical", "badge-warning badge-soft": a.severity !== "critical" }}>{a.severity}</span>
                            <span>{a.message}</span>
                            <Show when={a.componentId} fallback={<span class="font-mono text-xs text-base-content/60">{a.component}</span>}>
                              <button type="button" class="cursor-pointer font-mono text-xs text-base-content/80 hover:underline" onClick={() => navigate(`/components/${a.componentId}?zoom=1`)}>{a.component}</button>
                            </Show>
                            <span class="text-xs text-base-content/50">impairs {a.roleLabel} · {durationText(pageNow - Date.parse(a.raisedAt))}</span>
                          </div>
                        )}
                      </For>
                    </section>
                  </Show>
                  <div data-testid="health-history">
                    <HealthHistory transitions={health.data?.transitions ?? []} verdict={health.data?.verdict} />
                  </div>
                  <Show when={z().unconditional.length > 0}>
                    <div class="grid grid-cols-[repeat(auto-fill,minmax(15rem,1fr))] gap-3">
                      <For each={z().unconditional}>{(slot) => <SlotCard slot={slot} />}</For>
                    </div>
                  </Show>
                  <For each={z().choices}>
                    {(choice) => (
                      <section data-testid={`choice-${choice.name}`} class="flex flex-col gap-2">
                        <div class="flex items-baseline gap-2">
                          <span class="eyebrow">{choice.name}</span>
                          <Show when={choice.activeAlternate}>
                            <span class="text-[10.5px] text-base-content/50">built as {choice.activeAlternate}</span>
                          </Show>
                        </div>
                        <div class="grid grid-cols-[repeat(auto-fill,minmax(15rem,1fr))] gap-3">
                          <For each={choice.alternates.flatMap((a) => a.roles)}>{(slot) => <SlotCard slot={slot} />}</For>
                        </div>
                      </section>
                    )}
                  </For>
                </div>
                <Show when={z().noRole.length > 0}>
                  <section data-testid="no-role-strip" class="flex flex-wrap items-center gap-2 border-t border-base-300 px-4 py-3 text-xs text-base-content/60">
                    <span class="eyebrow">In the room, no role</span>
                    <For each={z().noRole}>
                      {(m) => (
                        <button
                          type="button"
                          class="cursor-pointer rounded-field border border-base-300 bg-base-100 px-2 py-0.5 font-mono text-[11px] text-base-content/80 hover:border-primary/50"
                          onClick={() => navigate(`/components/${m.componentId}?zoom=1`)}
                        >
                          {m.name}
                          <Show when={m.sharedWith.length > 0}>
                            <span class="opacity-60">{` · also ${m.sharedWith.join(", ")}`}</span>
                          </Show>
                        </button>
                      )}
                    </For>
                  </section>
                </Show>
              </div>
            )}
          </Show>
          </FleetShell>
        </Show>
      </Show>
    </Page>
  );

  function SlotCard(props: { slot: SlotVM }) {
    const s = () => props.slot;
    return (
      <div
        data-testid={`slot-${s().name}`}
        class="flex flex-col gap-2 rounded-box border bg-base-100 p-3"
        classList={{
          "border-error/50 bg-error/5": s().active && s().gap === "occupant-down" && s().impact === "outage",
          "border-warning/40 bg-warning/5": s().active && s().gap === "occupant-down" && s().impact !== "outage",
          "border-incomplete/45 bg-incomplete/5": s().active && s().gap === "unstaffed",
          "border-base-300": !s().active || s().gap === "none",
        }}
      >
        <div class="flex items-center justify-between gap-2">
          <span class="truncate text-sm font-semibold">{s().label}</span>
          {/* The gap kind is the card's one verdict-adjacent mark, and it is
              the server's distinction rendered, never recomputed: an active
              short role with nothing down is a commissioning gap (incomplete);
              one with a down occupant wears the failure it declared. An
              inactive role wears neither, because its figures did not
              contribute. */}
          <Show when={s().active && s().gap !== "none"}>
            <HealthBadge verdict={s().gap === "unstaffed" ? "incomplete" : s().impact} size="xs" />
          </Show>
        </div>
        <div class="text-xs tabular-nums text-base-content/60">
          {s().satisfying} of {s().quorum} satisfying
          <Show when={s().spare > 0}>{` + ${s().spare} spare`}</Show>
        </div>
        <Show when={s().occupants.length > 0}>
          <ul class="flex flex-col gap-1 text-xs">
            <For each={s().occupants}>
              {(o) => (
                <li class="flex items-center gap-2">
                  <span class="h-1.5 w-1.5 flex-none rounded-sm" classList={{ "bg-error": o.down, "bg-success": !o.down }} />
                  <Show when={o.componentId} fallback={<span class="font-mono text-[11px]" classList={{ "text-base-content/40 line-through": o.down }}>{o.name}</span>}>
                    <button
                      type="button"
                      class="cursor-pointer font-mono text-[11px] hover:underline"
                      classList={{ "text-base-content/40 line-through": o.down }}
                      onClick={(e) => {
                        e.stopPropagation();
                        navigate(`/components/${o.componentId}?zoom=1`);
                      }}
                    >
                      {o.name}
                    </button>
                  </Show>
                  <Show when={o.positionLabel}>
                    <span class="rounded border border-base-content/15 px-1 text-[10px]">{o.positionLabel}</span>
                  </Show>
                  <Show when={o.down}>
                    <span class="text-[10px] text-base-content/50">down</span>
                  </Show>
                  <Show when={o.sharedWith.length > 0}>
                    <span class="rounded border border-base-content/15 px-1 text-[10px]">also {o.sharedWith.join(", ")}</span>
                  </Show>
                </li>
              )}
            </For>
          </ul>
        </Show>
        <Show when={s().acceptedTypes.length > 0}>
          <div class="mt-auto pt-1 text-[10px] uppercase tracking-wider text-base-content/40">
            accepts {s().acceptedTypes.join(", ")}
            <Show when={s().pinnedProducts.length > 0}>{`, pinned ${s().pinnedProducts.join(", ")}`}</Show>
          </div>
        </Show>
      </div>
    );
  }
}
