import { type JSX, splitProps } from "solid-js";
import { Tooltip as K } from "@kobalte/core/tooltip";
import type { Placement } from "./placement";

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
  as?: "button" | "span" | "div";
  placement?: Placement;
  gutter?: number;
  openDelay?: number;
  closeDelay?: number;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  contentClass?: string;
  contentTestId?: string;
  children: JSX.Element;
};

export default function Tooltip(props: TooltipProps) {
  const [root, own, trigger] = splitProps(
    props,
    ["placement", "gutter", "openDelay", "closeDelay", "open", "onOpenChange"],
    ["content", "as", "contentClass", "contentTestId", "children"],
  );
  return (
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
  );
}
