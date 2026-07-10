import { type Component, type JSX, Show } from "solid-js";
import { Dynamic } from "solid-js/web";

// Button is the console's one button primitive. Every button goes through it so
// the intent class, size, icon size, leading gap, and the loading spinner are
// defined ONCE and cannot drift from one hand-written `<button class="btn ...">`
// to the next. Structural-only escapes (a raw `btn` with bespoke layout) are the
// rare exception, not the rule.
//
// - intent: the semantic vocabulary (app.css), never a raw daisyUI color class.
// - icon: pass the icon COMPONENT (not an element), so Button sizes it to match
//   the button size; it renders left of the label, or alone when `square`.
// - loading: swaps the icon for a spinner and disables the button.
// - square: icon-only; `label` becomes the aria-label.
// - class: an escape hatch for layout only (w-full, flex-none, self-start), never
//   for color/emphasis.
type Intent = "action" | "quiet" | "danger" | "warn" | "ok";
type Size = "md" | "sm" | "xs";

export default function Button(props: {
  intent?: Intent;
  size?: Size;
  icon?: Component<{ size?: number }>;
  iconTrailing?: boolean;
  loading?: boolean;
  square?: boolean;
  type?: "button" | "submit";
  disabled?: boolean;
  onClick?: (e: MouseEvent) => void;
  title?: string;
  // Accessible name. For a square (icon-only) button it is the only name, so set
  // it. For a labelled button it overrides the text name (rarely needed).
  label?: string;
  tabindex?: number;
  class?: string;
  children?: JSX.Element;
}) {
  const intent = () => props.intent ?? "quiet";
  const size = () => props.size ?? "sm";
  const iconPx = () => (size() === "xs" ? 14 : size() === "md" ? 16 : 15);
  const cls = () =>
    // md is daisyUI's default (no size class); sm / xs are explicit.
    ["btn", `btn-${intent()}`, size() === "md" ? null : `btn-${size()}`, props.square ? "btn-square" : "gap-1.5", props.class]
      .filter(Boolean)
      .join(" ");
  const Icon = () => props.icon;
  const iconEl = (
    <Show when={props.loading} fallback={<Show when={Icon()}>{(ic) => <Dynamic component={ic()} size={iconPx()} />}</Show>}>
      <span class="loading loading-spinner loading-xs" />
    </Show>
  );
  return (
    <button
      type={props.type ?? "button"}
      class={cls()}
      disabled={props.disabled || props.loading}
      onClick={(e) => props.onClick?.(e)}
      title={props.title}
      tabindex={props.tabindex}
      aria-label={props.label ?? (props.square ? props.title : undefined)}
    >
      <Show when={!props.iconTrailing}>{iconEl}</Show>
      <Show when={!props.square}>{props.children}</Show>
      <Show when={props.iconTrailing}>{iconEl}</Show>
    </button>
  );
}
