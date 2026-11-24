import { For, type JSX } from "solid-js";
import { Content, List, Root, Trigger } from "@kobalte/core/tabs";

// Tabs: a small set of views of one thing, one shown at a time (#2485), over
// Kobalte Tabs, in daisyUI's tab chrome. Kobalte owns the roles (tablist, tab,
// tabpanel and their links), roving focus and the keys: Tab lands on the
// active tab then moves into its panel, the arrows move between tabs and
// activate them, Home and End jump to the ends. Only the active panel is
// mounted. The page's own route tabs are TabRail (#2423), not this.
//
// tabs lists the choices: value (the key), label (any JSX: a count beside the
// name) and disabled. children renders the panel for the active value. label
// names the tab list for a screen reader. variant picks daisyUI's look: "box"
// (a pill track, the default) or "border" (an underline).
export interface TabItem {
  value: string;
  label: JSX.Element;
  disabled?: boolean;
}

export interface TabsProps {
  tabs: TabItem[];
  value: string;
  onChange: (value: string) => void;
  label: string;
  variant?: "box" | "border";
  size?: "xs" | "sm" | "md";
  class?: string;
  panelClass?: string;
  children: (value: string) => JSX.Element;
}

const VARIANT = { box: "tabs-box", border: "tabs-border" } as const;
const SIZE = { xs: "tabs-xs", sm: "tabs-sm", md: "tabs-md" } as const;

export default function Tabs(props: TabsProps) {
  return (
    <Root value={props.value} onChange={props.onChange} class={props.class}>
      <List aria-label={props.label} class={`tabs ${VARIANT[props.variant ?? "box"]} ${SIZE[props.size ?? "sm"]} w-fit`}>
        <For each={props.tabs}>
          {(t) => (
            <Trigger value={t.value} disabled={t.disabled} class="tab" classList={{ "tab-active": props.value === t.value }}>
              {t.label}
            </Trigger>
          )}
        </For>
      </List>
      <For each={props.tabs}>
        {(t) => (
          <Content value={t.value} class={props.panelClass ?? "focus:outline-none focus-visible:ring-1 focus-visible:ring-primary/60"}>
            {props.children(t.value)}
          </Content>
        )}
      </For>
    </Root>
  );
}
