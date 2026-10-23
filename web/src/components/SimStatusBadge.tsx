import { Show, type Component } from "solid-js";
import { Dynamic } from "solid-js/web";
import { CircleCheck, CircleDashed, OctagonX, Plug, Sliders, WifiOff } from "./icons";
import { simStatus, type SimComponent, type SimState } from "../lib/simulation";

// SimStatusBadge is a simulated component's status, on HealthBadge's recipe:
// every state wears its own glyph AND its word AND a hue, running wears the
// healthy green (#1067), and starting wears the incomplete look, a dashed
// ring on a cool hue, because a component the simulator has not picked up yet is
// unfinished, not broken. The two not-served states share the error hue but
// never a glyph: offline is the simulator gone quiet, failed is the simulator
// present and refusing this one component.
//
// The words are deliberately not up/down (reachability's verdict) or degraded
// (health's): this says what the simulator is doing, not what monitoring
// concluded. What an operator broke is not a status either, so it rides beside
// the status as its own warning chip: "unplugged", or the shaped traffic in a
// few words, with a muted note beneath when the simulator did not apply it.

const LOOK: Record<SimState, { badge: string; icon: Component<{ size?: number }>; hint: string }> = {
  offline: {
    badge: "badge-error",
    icon: WifiOff,
    hint: "Its simulator stopped reporting, so nothing answers at this address and collection sees it gone.",
  },
  failed: {
    badge: "badge-error",
    icon: OctagonX,
    hint: "The simulator was handed this component but could not start it: a port it cannot bind, or an address that is not loopback.",
  },
  starting: {
    badge: "badge-incomplete",
    icon: CircleDashed,
    hint: "Added since the simulator last pulled its list, or handed over moments ago. It runs once the simulator picks it up.",
  },
  running: {
    badge: "badge-healthy",
    icon: CircleCheck,
    hint: "The simulator answers for it at its address, as the real product would.",
  },
};

const UNPLUGGED_HINT = "Unplugged by an operator: dark on every protocol, so a collector sees it down at every layer, as a pulled cable would look.";
const SHAPED_HINT = "An operator shaped its traffic (latency, jitter, loss or a rate limit), and a collector sees exactly that.";
const NOT_APPLIED_HINT = "The impairment is set, but the simulator did not apply it: impairments are unavailable on its node, the change has not reached it yet, or the component is not running.";

export default function SimStatusBadge(props: {
  sim: Pick<SimComponent, "status" | "impairment" | "impaired">;
  size?: "xs" | "sm" | "md";
}) {
  const status = () => simStatus(props.sim);
  const look = () => LOOK[status().state];
  const size = () => `badge-${props.size ?? "sm"}`;
  const glyph = () => (props.size === "md" ? 14 : 12);
  return (
    <span class="flex min-w-0 max-w-full flex-col items-start gap-0.5" data-testid="sim-status-badge" data-state={status().state}>
      <span class="flex min-w-0 max-w-full flex-wrap items-center gap-1">
        <span class={`badge badge-soft ${look().badge} ${size()} shrink-0 gap-1 whitespace-nowrap`} title={look().hint} data-testid="sim-state">
          <Dynamic component={look().icon} size={glyph()} />
          {status().state}
        </span>
        <Show when={status().impairment}>
          {(i) => (
            <span
              class={`badge badge-soft badge-warning ${size()} min-w-0 gap-1 whitespace-nowrap`}
              title={i().unplugged ? UNPLUGGED_HINT : `${SHAPED_HINT} Set: ${i().summary}.`}
              data-testid="sim-impairment-chip"
            >
              <Dynamic component={i().unplugged ? Plug : Sliders} size={glyph()} />
              <span class={`truncate ${i().unplugged ? "" : "font-data"}`}>{i().summary}</span>
            </span>
          )}
        </Show>
      </span>
      <Show when={status().impairment && !status().impairment!.applied}>
        <span class="text-[11px] leading-tight text-base-content/50" title={NOT_APPLIED_HINT} data-testid="sim-not-applied">
          impairment not applied
        </span>
      </Show>
    </span>
  );
}
