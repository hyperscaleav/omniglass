import { Tooltip } from "@kobalte/core/tooltip";
import { Show } from "solid-js";
import { Info } from "./icons";

// InfoTip: the small (i) affordance that sits next to a field label and reveals
// help text on hover or keyboard focus, via a Kobalte tooltip. The content
// portals to the body so it is never clipped by the form drawer (the escape the
// column menu also needs). Replaces help text rendered muted under the field.
// The trigger must sit OUTSIDE the field's <label> (a labelable button inside it
// steals the label target and pollutes the control's accessible name).
//
// An optional href renders a "learn more" link at the end of the content and
// widens the close delay so the pointer can travel into the tooltip to click it.
export default function InfoTip(props: { text: string; label?: string; href?: string; hrefText?: string }) {
  return (
    <Tooltip openDelay={150} closeDelay={props.href ? 350 : 100} placement="top" gutter={4}>
      <Tooltip.Trigger
        type="button"
        aria-label={props.label ? `More about ${props.label}` : "More information"}
        class="inline-flex text-base-content/40 transition-colors hover:text-base-content/70 focus-visible:text-primary focus:outline-none"
      >
        <Info size={13} />
      </Tooltip.Trigger>
      <Tooltip.Portal>
        <Tooltip.Content class="z-100 max-w-56 rounded-box border border-base-300 bg-base-100 px-2.5 py-1.5 text-xs font-normal normal-case leading-snug tracking-normal text-base-content shadow-lg">
          {props.text}
          <Show when={props.href}>
            {" "}
            <a
              href={props.href}
              target="_blank"
              rel="noopener noreferrer"
              class="link link-primary whitespace-nowrap font-medium"
            >
              {props.hrefText ?? "Learn more"} &#8599;
            </a>
          </Show>
        </Tooltip.Content>
      </Tooltip.Portal>
    </Tooltip>
  );
}
