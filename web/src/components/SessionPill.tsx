import { Show, type Component } from "solid-js";
import { Dynamic } from "solid-js/web";
import { Ban, CircleCheck, CircleDashed, Clock, RefreshCw, WifiOff } from "./icons";
import { rel } from "../lib/format";
import { sessionWord, type SessionWord } from "../lib/session";
import type { ReachEndpoint } from "../lib/reachability";

// SessionPill is an endpoint's held session (#1059), on HealthBadge's recipe:
// every word wears its own glyph AND the word AND a soft hue, so the state
// survives a colour-blind reader. Establishing takes the incomplete look (a
// dashed ring: not yet, not broken), up the success green, recovering blue
// (the node is working on it), down red (gone, and the node keeps redialing on
// its own), and rejected amber with a refusal glyph: it is the one state the
// node will not leave without someone acting, so it reads apart from down.
//
// It is aged (ADR-0150): beside it, how long ago the state changed, and once
// the endpoint has delivered nothing for the staleness window the pill goes to
// the grey neutral chip and its hint says what it last read, since a state the
// node stopped reporting may no longer be true.
export const SESSION_LOOK: Record<SessionWord, { badge: string; icon: Component<{ size?: number }>; hint: string }> = {
  establishing: {
    badge: "badge-incomplete",
    icon: CircleDashed,
    hint: "The node is making the first connection: dialing, then logging in, then arming its listeners.",
  },
  up: {
    badge: "badge-soft badge-success",
    icon: CircleCheck,
    hint: "The connection is held and logged in; polls and commands borrow it rather than dialing their own.",
  },
  recovering: {
    badge: "badge-soft badge-info",
    icon: RefreshCw,
    hint: "The held connection dropped and the node is redialing on a backoff. Polls fault until it is back.",
  },
  down: {
    badge: "badge-soft badge-error",
    icon: WifiOff,
    hint: "Five attempts in a row failed. The node keeps redialing at a slow pace, so a component that returns comes back up on its own.",
  },
  rejected: {
    badge: "badge-soft badge-warning",
    icon: Ban,
    hint: "The component refused in a way retrying cannot fix, such as a wrong credential or a changed host key. The node dials no more until the endpoint changes or someone retries it.",
  },
  stale: {
    badge: "badge-ghost",
    icon: Clock,
    hint: "Nothing has arrived from this endpoint for a while, so the last state the node reported may no longer be true.",
  },
};

export default function SessionPill(props: { iface: ReachEndpoint; now: number }) {
  const word = () => sessionWord(props.iface, props.now);
  const hint = () => {
    const w = word();
    const s = props.iface.session;
    if (w === "stale" && s) return `Last read ${s.state} ${rel(s.ts, props.now)}. ${SESSION_LOOK.stale.hint}`;
    return w ? SESSION_LOOK[w].hint : "";
  };
  return (
    <Show when={word()}>
      {(w) => (
        <span class="inline-flex shrink-0 items-center gap-1.5">
          <span
            data-testid="session-pill"
            data-state={w()}
            class={`badge badge-sm shrink-0 gap-1 whitespace-nowrap ${SESSION_LOOK[w()].badge}`}
            title={hint()}
          >
            <Dynamic component={SESSION_LOOK[w()].icon} size={12} />
            {w() === "stale" ? props.iface.session!.state : w()}
          </span>
          <span data-testid="session-age" class="whitespace-nowrap text-[11px] tabular-nums text-base-content/50">
            {rel(props.iface.session!.ts, props.now)}
          </span>
        </span>
      )}
    </Show>
  );
}
