import { healthColor, healthLabel } from "../lib/og";

// HealthBadge: a dot + health label (up/degraded/down/unknown). Built but not
// wired to any live screen yet: health/state has no backend (a future
// component.state concern), so no entity feeds it until that lands.
export default function HealthBadge(props: { health: string }) {
  return (
    <span class="inline-flex items-center gap-2 text-sm">
      <span class="dot" style={{ background: healthColor[props.health] ?? "var(--unknown)" }} />
      <span class="text-base-content/70">{healthLabel(props.health)}</span>
    </span>
  );
}
