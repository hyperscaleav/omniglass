import { Show, type Component } from "solid-js";
import { Dynamic } from "solid-js/web";
import { useQuery } from "@tanstack/solid-query";
import { CircleCheck, CircleDashed, OctagonX, TriangleAlert } from "./icons";
import {
  locationHealth,
  locationHealthKey,
  systemHealth,
  systemHealthKey,
  verdictOf,
  type Verdict,
} from "../lib/health";

// HealthBadge is the console's one health chip. Four verdicts, four distinct
// states, never a single accent shade of "not fine": healthy, incomplete,
// degraded, and outage each get their own semantic hue AND their own glyph AND
// the word itself, so the state survives a colour-blind reader, a greyscale
// screenshot, and a printout. The word is the primary carrier; the colour and the
// shape only reinforce it.
//
// incomplete is deliberately the odd one out in both hue and glyph: a dashed ring
// on a cool indigo, against the solid warm glyphs the two failure verdicts wear.
// A room nobody has finished building has not broken, and an operator scanning a
// half-commissioned fleet must be able to tell the two apart at a glance without
// reading a word of it.
//
// It reads its verdict either from a value the caller already has (a location's
// system row, a health panel header, the systems list's bulk verdict map) or by
// fetching one itself, sharing the cache key with the panels.
//
// A LIST must always take the first path. The systems list took the second, one
// query per row, on the argument that there was no bulk health read; there is now
// (GET /systems:health, #653), and the fetching path is for a single badge with no
// caller who already knows. Note what makes the fetch happen: `system` or
// `location` present AND `verdict` absent. A list cell that passed an id while its
// bulk read was still in flight would put the per-row request back exactly where it
// was removed from, so the cell passes only the verdict.

const LOOK: Record<Verdict, { badge: string; icon: Component<{ size?: number }>; hint: string }> = {
  healthy: { badge: "badge-success", icon: CircleCheck, hint: "Every role this system needs is filled." },
  incomplete: { badge: "badge-incomplete", icon: CircleDashed, hint: "A role is short because its hardware was never installed. A commissioning gap, not a fault: no alarm will ever fire for it." },
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
