import { For, Show } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import HealthBadge from "./HealthBadge";
import BandCanvas from "./BandCanvas";
import { impairedRoles, quorumLabel, systemHealth, systemHealthKey } from "../lib/health";
import { entityLabel } from "../lib/entities";
import type { SystemCluster } from "../lib/fleet";

// One system on the location zoom (#635): the verdict the server recorded,
// the component dot strip (the same canvas the fleet zoom paints, one
// cluster), and the quorum arithmetic in the server's own terms. The card
// renders figures and never computes one: an unstaffed role arrives as
// incomplete from the rollup, and the impact word appears only where the
// server contributed it.
export default function SystemCard(props: { cluster: SystemCluster; onOpen?: (systemId: string) => void }) {
  const health = useQuery(() => ({
    queryKey: systemHealthKey(props.cluster.systemId),
    queryFn: () => systemHealth(props.cluster.systemId),
  }));

  return (
    <div
      data-testid={`syscard-${props.cluster.systemId}`}
      class="flex w-64 flex-none flex-col gap-2 rounded-lg border border-base-content/10 p-3"
    >
      <button
        type="button"
        class="flex cursor-pointer items-center gap-2 text-left"
        onClick={() => props.onOpen?.(props.cluster.systemId)}
      >
        <HealthBadge verdict={props.cluster.verdict ?? undefined} size="xs" />
        <span class="truncate text-sm font-medium">{props.cluster.label}</span>
      </button>
      <BandCanvas clusters={[props.cluster]} ariaLabel={`${props.cluster.label}: ${props.cluster.dots.length} components`} />
      <Show when={health.data}>
        {(h) => (
          <ul class="flex flex-col gap-0.5 text-xs text-base-content/60">
            <For each={impairedRoles(h())}>
              {(role) => (
                <li>
                  {entityLabel(role)}: {quorumLabel(role)}
                </li>
              )}
            </For>
          </ul>
        )}
      </Show>
    </div>
  );
}
