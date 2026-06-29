import { Show } from "solid-js";
import { resolvePalette, type PaletteEntry } from "../lib/og";

// Badge: a palette-driven status chip. Pass a palette (banded for numbers, exact
// for strings) or an explicit color. Colors come from tokens via the palette.
export default function Badge(props: {
  value: string | number | null | undefined;
  palette?: PaletteEntry[];
  color?: string;
  label?: string;
  dot?: boolean;
}) {
  const entry = () => (props.value == null ? null : resolvePalette(props.palette, props.value));
  const color = () => props.color ?? entry()?.color ?? "var(--unknown)";
  const text = () => props.label ?? entry()?.label ?? String(props.value);
  return (
    <Show when={props.value != null}>
      <span
        class="badge"
        style={{
          color: color(),
          "border-color": props.color ? color() : `color-mix(in oklch, ${color()} 45%, transparent)`,
          background: `color-mix(in oklch, ${color()} 13%, transparent)`,
        }}
      >
        <Show when={props.dot ?? true}>
          <span class="dot" style={{ background: color() }} />
        </Show>
        {text()}
      </span>
    </Show>
  );
}
