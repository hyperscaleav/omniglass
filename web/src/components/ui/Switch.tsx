import { type JSX, Show } from "solid-js";
import { Control, Description, Input, Label, Root, Thumb } from "@kobalte/core/switch";

// Switch: an on/off setting that takes effect as a state, not a choice
// submitted with others (#2485), over Kobalte Switch. A real (visually hidden)
// checkbox with role="switch" carries the focus, the name and the state, so a
// screen reader says "on" or "off"; Space toggles it. The track and thumb are
// drawn here in the house teal. A checkbox in a list of choices (which kinds
// a tag applies to) stays a native checkbox.
//
// label is the switch's name, always rendered (sr-only with hideLabel, when
// the row's own label names it); description is muted help under it.
export interface SwitchProps {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label: JSX.Element;
  hideLabel?: boolean;
  description?: JSX.Element;
  disabled?: boolean;
  size?: "sm" | "md";
  name?: string;
  class?: string;
}

export default function Switch(props: SwitchProps) {
  const sm = () => (props.size ?? "sm") === "sm";
  return (
    <Root
      checked={props.checked}
      onChange={props.onChange}
      disabled={props.disabled}
      name={props.name}
      class={props.class ?? "inline-flex items-center gap-2 data-[disabled]:opacity-50"}
    >
      {/* The labelable part, so a FieldRow label names it too. */}
      <Input class="peer" data-field-target="" />
      <Control
        class="relative inline-flex flex-none cursor-pointer items-center rounded-full border border-base-content/25 bg-base-300 transition-colors data-[checked]:border-primary data-[checked]:bg-primary peer-focus-visible:ring-2 peer-focus-visible:ring-primary/60 peer-focus-visible:ring-offset-1 peer-focus-visible:ring-offset-base-100 data-[disabled]:cursor-not-allowed"
        classList={{ "h-5 w-9": sm(), "h-6 w-11": !sm() }}
      >
        <Thumb
          class="block rounded-full bg-base-content/70 shadow transition-transform data-[checked]:bg-primary-content"
          classList={{
            "h-3.5 w-3.5 translate-x-0.5 data-[checked]:translate-x-[1.05rem]": sm(),
            "h-4.5 w-4.5 translate-x-0.5 data-[checked]:translate-x-[1.3rem]": !sm(),
          }}
        />
      </Control>
      <span class="flex flex-col" classList={{ "sr-only": !!props.hideLabel }}>
        <Label class="cursor-pointer text-sm">{props.label}</Label>
        <Show when={props.description}>
          <Description class="text-xs text-base-content/50">{props.description}</Description>
        </Show>
      </span>
    </Root>
  );
}
