import { Index, Show } from "solid-js";
import InfoTip from "./InfoTip";
import { Ban, ChevronRight } from "./icons";
import { ladder, rungHint, type Rung } from "../lib/session";
import type { ReachEndpoint } from "../lib/reachability";

// ReachLadder is the layered gate made visible (#1059, ADR-0158): an
// endpoint's rungs bottom up, each a chip that teaches itself on hover or
// focus (what the rung is, then what it says now), so the ladder is learned
// one rung at a time rather than from a paragraph. The ping's chip is dashed:
// it is informational and never gates, so a failed ping beside an answering
// transport reads as a fact, not a fault. A rung a refusal holds wears the
// refusal glyph, and the rungs above it read untried.
//
// The rungs iterate by position (Index), not by the objects a read hands back:
// the component's view polls, and a fresh object per poll would rebuild each
// chip, tearing an open tooltip out from under the pointer every five seconds.

function Dot(props: { rung: Rung }) {
  const cls = () => {
    const r = props.rung;
    if (r.status === "pass") return "bg-success";
    if (r.status === "fail") return r.role === "informational" ? "border border-error" : "bg-error";
    return "border border-base-content/35";
  };
  return (
    <Show when={props.rung.status !== "held"} fallback={<span class="text-warning"><Ban size={10} /></span>}>
      <span class={`inline-block size-1.5 shrink-0 rounded-full ${cls()}`} />
    </Show>
  );
}

const chip = (r: Rung) =>
  [
    "inline-flex items-center gap-1 rounded-field border px-1.5 py-0.5 text-[10.5px] leading-none whitespace-nowrap",
    "transition-colors hover:bg-base-content/5 focus:outline-none focus-visible:ring-1 focus-visible:ring-primary",
    r.status === "held" ? "border-warning/50 text-warning" : "text-base-content/70",
    r.status !== "held" && (r.role === "informational" ? "border-dashed border-base-content/25" : "border-base-content/15"),
    r.status === "untried" || r.status === "unknown" ? "text-base-content/45" : "",
  ]
    .filter(Boolean)
    .join(" ");

export default function ReachLadder(props: { iface: ReachEndpoint }) {
  const rungs = () => ladder(props.iface);
  return (
    <div class="inline-flex shrink-0 items-center gap-0.5" role="group" aria-label="Reachability ladder, bottom up">
      <Index each={rungs()}>
        {(r, i) => (
          <>
            <Show when={i > 0}>
              <span class="text-base-content/25"><ChevronRight size={10} /></span>
            </Show>
            <InfoTip text={rungHint(r())} label={`${r().key}: ${r().status}${r().role === "informational" ? " (informational)" : ""}`} class={chip(r())}>
              <span data-testid="ladder-rung" data-rung={r().key} data-status={r().status} data-role={r().role} class="inline-flex items-center gap-1">
                <Dot rung={r()} />
                {r().key}
              </span>
            </InfoTip>
          </>
        )}
      </Index>
    </div>
  );
}
