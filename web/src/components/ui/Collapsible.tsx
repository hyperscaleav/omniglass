import { type JSX } from "solid-js";
import { Content, Root, Trigger } from "@kobalte/core/collapsible";
import { ChevronRight } from "../icons";

// Collapsible: a section whose body opens and closes under its heading
// (#2485), over Kobalte Collapsible. The trigger is a real button carrying
// aria-expanded and aria-controls; Enter or Space toggles it and the focus
// stays on it. A closed body is unmounted. Where a plain disclosure with no
// state of its own is enough (a "show more" in prose), a native <details>
// stays native; this is for a section the console opens and closes itself or
// whose state a test or a caller reads (a cleared-alarms list, a long list of
// facts that is closed by default).
//
// title is the trigger's text (the section's name; a count beside it is
// fine). open/onOpenChange control it; defaultOpen starts it open
// uncontrolled. The chevron turns with the state.
export interface CollapsibleProps {
  title: JSX.Element;
  open?: boolean;
  defaultOpen?: boolean;
  onOpenChange?: (open: boolean) => void;
  disabled?: boolean;
  class?: string;
  triggerClass?: string;
  contentClass?: string;
  children: JSX.Element;
}

export default function Collapsible(props: CollapsibleProps) {
  return (
    <Root open={props.open} defaultOpen={props.defaultOpen} onOpenChange={props.onOpenChange} disabled={props.disabled} class={props.class}>
      <Trigger
        class={
          props.triggerClass ??
          "group flex cursor-pointer items-center gap-1 rounded-field text-left focus:outline-none focus-visible:ring-1 focus-visible:ring-primary/60"
        }
      >
        <span class="flex-none text-base-content/50 transition-transform group-data-[expanded]:rotate-90">
          <ChevronRight size={13} />
        </span>
        {props.title}
      </Trigger>
      <Content class={props.contentClass}>{props.children}</Content>
    </Root>
  );
}
