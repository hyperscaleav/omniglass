import { Show, type Component } from "solid-js";
import { Dynamic } from "solid-js/web";
import { useQuery } from "@tanstack/solid-query";
import { CircleCheck, CircleDashed, OctagonX, TriangleAlert } from "./icons";
import {
  locationHealth,
  locationHealthKey,
  systemHealth,
  systemHealthKey,
  healthOf,
  type HealthState,
} from "../lib/health";
import Tooltip from "./ui/Tooltip";

// HealthBadge is the console's one health chip. Four health results, four distinct
// states, never a single accent shade of "not fine": incomplete, degraded and
// outage each get their own semantic hue, healthy wears green (#1067; the grey
// #872 gave it read as unknown), and every one carries its own glyph AND the word itself, so the state survives a colour-blind reader, a greyscale
// screenshot, and a printout. The word is the primary carrier; the colour and the
// shape only reinforce it.
//
// incomplete is deliberately the odd one out in both hue and glyph: a dashed ring
// on a cool indigo, against the solid warm glyphs the two failure health results wear.
// A room nobody has finished building has not broken, and an operator scanning a
// half-commissioned fleet must be able to tell the two apart at a glance without
// reading a word of it.
//
// It reads its health either from a value the caller already has (a location's
// system row, a health panel header, the systems list's bulk health map) or by
// fetching one itself, sharing the cache key with the panels.
//
// A LIST must always take the first path. The systems list took the second, one
// query per row, on the argument that there was no bulk health read; there is now
// (GET /systems:health, #653), and the fetching path is for a single badge with no
// caller who already knows. Note what makes the fetch happen: `system` or
// `location` present AND `health` absent. A list cell that passed an id while its
// bulk read was still in flight would put the per-row request back exactly where it
// was removed from, so the cell passes only the health.

const LOOK: Record<HealthState, { badge: string; icon: Component<{ size?: number }>; hint: string }> = {
  healthy: { badge: "badge-healthy", icon: CircleCheck, hint: "Every slot this system needs is filled." },
  incomplete: { badge: "badge-incomplete", icon: CircleDashed, hint: "A slot is short because its hardware was never installed. A commissioning gap, not a fault: no alarm will ever fire for it." },
  degraded: { badge: "badge-warning", icon: TriangleAlert, hint: "A slot is impaired, but the system is still up." },
  outage: { badge: "badge-error", icon: OctagonX, hint: "An impaired slot takes this out of service." },
};

export default function HealthBadge(props: {
  // A health the caller already holds. When set, the badge never fetches.
  health?: string;
  // Or the thing to read a health for: exactly one of these.
  system?: string;
  location?: string;
  size?: "xs" | "sm" | "md";
  // Render nothing at all until a health is known, rather than an "unknown" chip.
  // The list cell wants this so a page of rows does not flash a column of unknowns.
  quiet?: boolean;
}) {
  const fetching = () => !props.health && !!(props.system || props.location);
  const q = useQuery(() => ({
    queryKey: props.system ? systemHealthKey(props.system) : locationHealthKey(props.location ?? ""),
    queryFn: () => (props.system ? systemHealth(props.system) : locationHealth(props.location!)),
    enabled: fetching(),
    staleTime: 30_000,
    retry: false,
    refetchOnWindowFocus: false,
  }));
  const state = () => healthOf(props.health ?? q.data?.health);
  const size = () => `badge-${props.size ?? "sm"}`;

  return (
    <Show
      when={state()}
      fallback={
        <Show when={!props.quiet}>
          <Tooltip as="span" tabIndex={0} class={`badge badge-ghost ${size()} gap-1`} content="No health has been read for this yet.">
            unknown
          </Tooltip>
        </Show>
      }
    >
      {(v) => (
        <Tooltip as="span" tabIndex={0} class={`badge badge-soft ${LOOK[v()].badge} ${size()} gap-1`} content={LOOK[v()].hint}>
          <Dynamic component={LOOK[v()].icon} size={props.size === "md" ? 14 : 12} />
          {v()}
        </Tooltip>
      )}
    </Show>
  );
}
