import { type JSX, Show, splitProps } from "solid-js";
import { Dynamic } from "solid-js/web";
import { Tooltip as K } from "@kobalte/core/tooltip";
import type { Placement } from "./placement";
import type { Component } from "solid-js";

// Tooltip: help text revealed on hover or keyboard focus (#2485), over Kobalte
// Tooltip. The content portals to the body, so a card, the grid or a drawer
// never clips it; the trigger gets aria-describedby to it. children is the
// trigger's content and every other attribute (class, aria-label, tabIndex,
// data-*, style, title) lands on the trigger.
//
// The trigger is a <button type="button"> by default: focusable, so a keyboard
// user reaches the help. as="span" renders a non-button trigger (a chip, a map
// slot); give it tabIndex={0} when it should take the focus. Never put a
// Tooltip trigger inside a <label> (it steals the label and kills hover: the
// kobalte skill's first gotcha); place it beside the label instead.
//
// open/onOpenChange control it (a tip that only shows when its text is cut
// off). contentClass replaces the house card when a tip holds more than text.
export const TOOLTIP_CLASS =
  "z-100 max-w-56 rounded-box border border-base-300 bg-base-100 px-2.5 py-1.5 text-xs font-normal normal-case leading-snug tracking-normal text-base-content shadow-lg";

export type TooltipProps = Omit<JSX.HTMLAttributes<HTMLElement>, "content"> & {
  content: JSX.Element;
  as?: keyof JSX.IntrinsicElements | Component<any>;
  type?: "button" | "submit";
  disabled?: boolean;
  placement?: Placement;
  gutter?: number;
  openDelay?: number;
  closeDelay?: number;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  contentClass?: string;
  contentTestId?: string;
  children?: JSX.Element;
  // as={A} and the like forward their own props (href, activeClass, target).
  [attr: string]: any;
};

export default function Tooltip(props: TooltipProps) {
  const [root, own, trigger] = splitProps(
    props,
    ["placement", "gutter", "openDelay", "closeDelay", "open", "onOpenChange"],
    ["content", "as", "contentClass", "contentTestId", "children"],
  );
  const [, bare] = splitProps(trigger, ["tabIndex"]);
  // An empty tip (a conditional that resolved to nothing) is no tooltip: the
  // trigger renders bare, without the tabIndex a tip would earn.
  return (
    <Show
      when={own.content}
      fallback={
        <Dynamic component={own.as ?? "button"} {...bare} type={(own.as ?? "button") === "button" ? "button" : undefined}>
          {own.children}
        </Dynamic>
      }
    >
      <K
        placement={root.placement ?? "top"}
        gutter={root.gutter ?? 4}
        openDelay={root.openDelay ?? 150}
        closeDelay={root.closeDelay ?? 100}
        open={root.open}
        onOpenChange={root.onOpenChange}
      >
        <K.Trigger as={own.as ?? "button"} type={(own.as ?? "button") === "button" ? "button" : undefined} {...trigger}>
          {own.children}
        </K.Trigger>
        <K.Portal>
          <K.Content class={own.contentClass ?? TOOLTIP_CLASS} data-testid={own.contentTestId}>{own.content}</K.Content>
        </K.Portal>
      </K>
    </Show>
  );
}
