import { type JSX, splitProps } from "solid-js";
import { Popover as K } from "@kobalte/core/popover";
import type { Placement } from "./placement";

// Popover: a floating panel opened by a button (#2485), over Kobalte Popover:
// a menu of choices, the column picker. Kobalte owns open on click, Esc and
// outside-click to close, focus into the panel and back to the trigger on
// close; this portals the panel so the grid card or a drawer never clips it
// (why ColumnMenu left a daisyUI in-flow dropdown).
//
// children is the trigger's content and every other attribute (class,
// aria-label, title, disabled) lands on the trigger, a <button>. content is
// the panel; contentClass styles it (the house menu card by default) and
// contentLabel names it. open/onOpenChange control it, so a choice in the
// panel can close it. initialFocus picks what takes the focus as the panel
// opens (the choice already made, #2304), else Kobalte's first focusable.
export const POPOVER_CLASS = "z-50 rounded-box border border-base-300 bg-base-100 p-1 shadow-2xl focus:outline-none";

export type PopoverProps = Omit<JSX.ButtonHTMLAttributes<HTMLButtonElement>, "content"> & {
  content: JSX.Element;
  placement?: Placement;
  gutter?: number;
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  contentClass?: string;
  contentLabel?: string;
  initialFocus?: (panel: HTMLElement) => HTMLElement | null | undefined;
  children: JSX.Element;
};

export default function Popover(props: PopoverProps) {
  const [root, own, trigger] = splitProps(
    props,
    ["placement", "gutter", "open", "onOpenChange"],
    ["content", "contentClass", "contentLabel", "initialFocus", "children"],
  );
  return (
    <K placement={root.placement ?? "bottom-end"} gutter={root.gutter ?? 4} open={root.open} onOpenChange={root.onOpenChange}>
      <K.Trigger {...trigger}>{own.children}</K.Trigger>
      <K.Portal>
        <K.Content
          class={own.contentClass ?? POPOVER_CLASS}
          aria-label={own.contentLabel}
          onOpenAutoFocus={(e) => {
            const target = own.initialFocus?.(e.currentTarget as HTMLElement);
            if (target) {
              e.preventDefault();
              target.focus();
            }
          }}
        >
          {own.content}
        </K.Content>
      </K.Portal>
    </K>
  );
}
