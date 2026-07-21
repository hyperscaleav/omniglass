import { Show, type Component } from "solid-js";
import { Dynamic } from "solid-js/web";
import { useQuery } from "@tanstack/solid-query";
import { CircleCheck, OctagonX, TriangleAlert } from "./icons";
import {
  locationHealth,
  locationHealthKey,
  systemHealth,
  systemHealthKey,
  verdictOf,
  type Verdict,
} from "../lib/health";

// HealthBadge is the console's one health chip. Three verdicts, three distinct
// states, never a single accent shade of "not fine": healthy, degraded, and outage
// each get their own semantic hue AND their own glyph AND the word itself, so the
// state survives a colour-blind reader, a greyscale screenshot, and a printout. The
// word is the primary carrier; the colour and the shape only reinforce it.
//
// It reads its verdict either from a value the caller already has (a location's
// system row, a health panel header) or by fetching one itself, which is what the
// systems list uses per row: there is no bulk health read, so the badge owns the
// query and the cache key it shares with the panels.

const LOOK: Record<Verdict, { badge: string; icon: Component<{ size?: number }>; hint: string }> = {
  healthy: { badge: "badge-success", icon: CircleCheck, hint: "Every role this system needs is filled." },
  degraded: { badge: "badge-warning", icon: TriangleAlert, hint: "A role is impaired, but the system is still up." },
  outage: { badge: "badge-error", icon: OctagonX, hint: "An impaired role takes this out of service." },
};

export default function HealthBadge(props: {
  // A verdict the caller already holds. When set, the badge never fetches.
  verdict?: string;
  // Or the thing to read a verdict for: exactly one of these.
  system?: string;
  location?: string;
  size?: "xs" | "sm" | "md";
  // Render nothing at all until a verdict is known, rather than an "unknown" chip.
  // The list cell wants this so a page of rows does not flash a column of unknowns.
  quiet?: boolean;
}) {
  const fetching = () => !props.verdict && !!(props.system || props.location);
  const q = useQuery(() => ({
    queryKey: props.system ? systemHealthKey(props.system) : locationHealthKey(props.location ?? ""),
    queryFn: () => (props.system ? systemHealth(props.system) : locationHealth(props.location!)),
    enabled: fetching(),
    staleTime: 30_000,
    retry: false,
    refetchOnWindowFocus: false,
  }));
  const verdict = () => verdictOf(props.verdict ?? q.data?.verdict);
  const size = () => `badge-${props.size ?? "sm"}`;

  return (
    <Show
      when={verdict()}
      fallback={
        <Show when={!props.quiet}>
          <span class={`badge badge-ghost ${size()} gap-1`} title="No health has been read for this yet.">
            unknown
          </span>
        </Show>
      }
    >
      {(v) => (
        <span class={`badge badge-soft ${LOOK[v()].badge} ${size()} gap-1`} title={LOOK[v()].hint}>
          <Dynamic component={LOOK[v()].icon} size={props.size === "md" ? 14 : 12} />
          {v()}
        </span>
      )}
    </Show>
  );
}
