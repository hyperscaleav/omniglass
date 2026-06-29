// Domain display helpers: the palette resolver behind Badge, and the health
// language. Colors are token names (never hardcoded hex), per the design
// system. Health/severity have no backend yet (a future component.state
// concern); the helpers exist so the primitives are ready, but no live screen
// feeds them mock health.

export type PaletteEntry = { value: string | number; label?: string; color: string };

// resolvePalette maps a value to its palette entry: numeric palettes are banded
// (the highest threshold the value meets); string palettes match exactly.
export function resolvePalette(palette: PaletteEntry[] | undefined, value: string | number): PaletteEntry | null {
  if (!palette || !palette.length) return null;
  if (typeof value === "number") {
    let best: PaletteEntry | null = null;
    for (const e of palette) {
      const n = Number(e.value);
      if (value >= n && (!best || n > Number(best.value))) best = e;
    }
    return best;
  }
  return palette.find((e) => String(e.value) === String(value)) ?? null;
}

export type Health = "up" | "degraded" | "down" | "unknown";

export const healthColor: Record<string, string> = {
  up: "var(--up)",
  degraded: "var(--warn)",
  down: "var(--high)",
  unknown: "var(--unknown)",
};

export const healthLabel = (h: string): string =>
  ({ up: "Up", degraded: "Degraded", down: "Down" } as Record<string, string>)[h] ?? "Unknown";
