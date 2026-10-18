import type { JSX } from "solid-js";
import { useSeverity } from "../lib/severity";

// SeverityBadge is the console's one severity chip (#917). It takes a level NAME
// as it rides an alarm and renders the registry's label in the registry's color,
// with a hint that teaches what the level does to its component (an outage level
// takes it out, any other degrades it). An unknown name, or any name while the
// levels are still loading, renders as itself in the neutral chip.
export default function SeverityBadge(props: { name: string; size?: "xs" | "sm"; class?: string }): JSX.Element {
  const scale = useSeverity();
  const look = () => scale().look(props.name);
  const hint = () =>
    !look().known
      ? undefined
      : look().outage
        ? `${look().label}: an active alarm at this level puts its component in outage`
        : `${look().label}: an active alarm at this level degrades its component`;
  return (
    <span
      class={`badge badge-${props.size ?? "sm"} shrink-0 ${look().badge} ${props.class ?? ""}`}
      data-severity={props.name}
      title={hint()}
    >
      {look().label}
    </span>
  );
}
