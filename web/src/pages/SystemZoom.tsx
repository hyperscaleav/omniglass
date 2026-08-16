import { For, Show, createEffect, createMemo } from "solid-js";
import { useNavigate, useParams } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import Breadcrumb from "../components/Breadcrumb";
import HealthBadge from "../components/HealthBadge";
import ZoomLadder from "../components/ZoomLadder";
import ZoomRail from "../components/ZoomRail";
import { railModel } from "../lib/zoom_rail";
import { FLEET_VIEW_KEY, ancestors, fleetView, locationIndex } from "../lib/fleet";
import { systemHealth, systemHealthKey } from "../lib/health";
import { systemRoles, systemRolesKey } from "../lib/system_roles";
import { systemZoomVM, type SlotVM } from "../lib/system_zoom";
import { zoomChips } from "../lib/zoom";
import { entityLabel } from "../lib/entities";
import { describeError } from "../lib/format";

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

  const vm = createMemo(() => {
    if (!view.data || !health.data || !declared.data) return undefined;
    return systemZoomVM(health.data, declared.data, view.data, id());
  });

  const chips = createMemo(() =>
    view.data ? zoomChips("system", { locationId: system()?.location ?? null, systemId: id() }, view.data) : [],
  );

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
      ...(system() ? [{ key: system()!.id, label: entityLabel(system()!) }] : []),
    ];
  });

  const pending = () => view.isPending || health.isPending || declared.isPending;
  const failed = () => view.isError || health.isError || declared.isError;

  return (
    <Page
      title={system() ? entityLabel(system()!) : "System"}
      subtitle={health.data?.verdict ? "" : ""}
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
          <ZoomLadder
            chips={chips()}
            hint="One card per role the standard declares."
            onSelect={(chip) => {
              if (chip.id === "fleet") navigate("/fleet");
              if (chip.id === "location" && system()?.location) navigate(`/locations/${system()!.location}?zoom=1`);
            }}
          />
          <div class="flex gap-6">
          <Show when={vm()}>
            {(z) => (
              <div class="flex min-w-0 flex-1 flex-col gap-6">
                <Show when={z().unconditional.length > 0}>
                  <div class="grid grid-cols-[repeat(auto-fill,minmax(16rem,1fr))] gap-3">
                    <For each={z().unconditional}>{(slot) => <SlotCard slot={slot} />}</For>
                  </div>
                </Show>
                <For each={z().choices}>
                  {(choice) => (
                    <section data-testid={`choice-${choice.name}`} class="flex flex-col gap-2">
                      <h2 class="text-sm font-semibold capitalize">{choice.name}</h2>
                      <div class="flex flex-col gap-3">
                        <For each={choice.alternates}>
                          {(alt) => (
                            <div
                              data-testid={`alternate-${choice.name}-${alt.name}`}
                              class="rounded-lg border p-3"
                              classList={{
                                "border-base-content/15": alt.active,
                                "border-dashed border-base-content/10 opacity-70": !alt.active,
                              }}
                            >
                              <div class="mb-2 flex items-center gap-2 text-xs">
                                <span class="font-medium">{alt.name}</span>
                                <Show
                                  when={alt.active}
                                  fallback={<span class="text-base-content/50">not the build in use</span>}
                                >
                                  <span class="rounded-md border border-base-content/20 px-1.5 py-0.5">in use</span>
                                </Show>
                              </div>
                              <div class="grid grid-cols-[repeat(auto-fill,minmax(16rem,1fr))] gap-3">
                                <For each={alt.roles}>{(slot) => <SlotCard slot={slot} />}</For>
                              </div>
                            </div>
                          )}
                        </For>
                      </div>
                    </section>
                  )}
                </For>
                <Show when={z().noRole.length > 0}>
                  <section data-testid="no-role-strip" class="flex flex-wrap items-center gap-2 border-t border-base-content/10 pt-3 text-xs text-base-content/60">
                    <span>Members with no role:</span>
                    <For each={z().noRole}>
                      {(m) => (
                        <span class="rounded-md border border-base-content/15 px-1.5 py-0.5">
                          {m.name}
                          <Show when={m.sharedWith.length > 0}>
                            <span class="opacity-60">{` · also ${m.sharedWith.join(", ")}`}</span>
                          </Show>
                        </span>
                      )}
                    </For>
                  </section>
                </Show>
              </div>
            )}
          </Show>
          <Show when={view.data}>{(v) => <ZoomRail model={railModel({ zoom: "system", systemId: id() }, v())} />}</Show>
          </div>
        </Show>
      </Show>
    </Page>
  );

  function SlotCard(props: { slot: SlotVM }) {
    const s = () => props.slot;
    return (
      <div
        data-testid={`slot-${s().name}`}
        class="flex flex-col gap-2 rounded-lg border p-3"
        classList={{
          "border-base-content/10": s().active,
          "border-dashed border-base-content/10": !s().active,
        }}
      >
        <div class="flex items-center justify-between gap-2">
          <span class="truncate text-sm font-medium">{s().label}</span>
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
        <div class="text-xs text-base-content/60">
          {s().satisfying} of {s().quorum} satisfying
        </div>
        <Show when={s().occupants.length > 0}>
          <ul class="flex flex-col gap-1 text-xs">
            <For each={s().occupants}>
              {(o) => (
                <li class="flex items-center gap-2">
                  <span classList={{ "text-base-content/40 line-through": o.down }}>{o.name}</span>
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
          <div class="text-[10px] uppercase tracking-wider text-base-content/40">
            accepts {s().acceptedTypes.join(", ")}
            <Show when={s().pinnedProducts.length > 0}>{`, pinned ${s().pinnedProducts.join(", ")}`}</Show>
          </div>
        </Show>
      </div>
    );
  }
}
