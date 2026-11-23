import { For, type JSX } from "solid-js";
import { Item, ItemInput, ItemLabel, Label, Root } from "@kobalte/core/segmented-control";

// SegmentedControl: one of a few mutually exclusive modes, all visible at once
// (#2485), over Kobalte SegmentedControl: a radio group under the tab chrome
// (daisyUI tabs-box). Tab lands on the chosen segment, the arrows move and
// choose, the focus stays in the group. Use it for a mode or a filter that
// switches what one view shows ("From scratch" or "From a template"; open or
// cleared), not for views with panels of their own (Tabs) and not for an
// on/off setting (Switch). Two to five segments; more is a Select.
//
// options lists the segments (value, label, disabled); label names the group
// for a screen reader (rendered sr-only unless showLabel).
export interface SegmentOption {
  value: string;
  label: JSX.Element;
  disabled?: boolean;
}

export interface SegmentedControlProps {
  options: SegmentOption[];
  value: string;
  onChange: (value: string) => void;
  label: string;
  showLabel?: boolean;
  size?: "xs" | "sm" | "md";
  disabled?: boolean;
  class?: string;
  "data-testid"?: string;
}

// Undo Kobalte's visually-hidden inline style; the class keeps it invisible.
const COVER = { clip: "auto", "clip-path": "none", width: "100%", height: "100%", margin: "0", overflow: "visible", position: "absolute", "white-space": "normal" } as const;

const SIZE = { xs: "tabs-xs", sm: "tabs-sm", md: "tabs-md" } as const;

export default function SegmentedControl(props: SegmentedControlProps) {
  return (
    <Root
      value={props.value}
      onChange={props.onChange}
      disabled={props.disabled}
      class={props.class ?? "inline-flex items-center gap-2"}
      data-testid={props["data-testid"]}
    >
      <Label classList={{ "sr-only": !props.showLabel, "text-xs text-base-content/60": !!props.showLabel }}>{props.label}</Label>
      {/* role=presentation on the wrapper between the root and its items: a
          Chromium screen-reader bug otherwise drops the radios (Kobalte docs). */}
      <div role="presentation" class={`tabs tabs-box ${SIZE[props.size ?? "sm"]} w-fit`}>
        <For each={props.options}>
          {(o) => (
            <Item
              value={o.value}
              disabled={o.disabled}
              class="tab relative has-[input:focus-visible]:ring-1 has-[input:focus-visible]:ring-primary/60"
              classList={{ "tab-active": props.value === o.value }}
            >
              {/* The radio covers its segment, transparent, instead of being
                  visually hidden: a click (a person's or a test driver's)
                  lands on the radio itself, not on a label in front of it. */}
              <ItemInput class="absolute inset-0 m-0 h-full w-full cursor-pointer opacity-0" style={COVER} />
              <ItemLabel class="cursor-pointer">{o.label}</ItemLabel>
            </Item>
          )}
        </For>
      </div>
    </Root>
  );
}
