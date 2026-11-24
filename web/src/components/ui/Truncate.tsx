import type { JSX } from "solid-js";
import Tooltip from "./Tooltip";

// Truncate: a value cut to one line that shows its full text on hover and on
// keyboard focus (#2486). The one helper for every truncated value, so no cell
// leans on the native title attribute. The tip only exists when there is text.
export default function Truncate(props: { text: string | undefined; class?: string; as?: keyof JSX.IntrinsicElements }) {
  return (
    <Tooltip as={props.as ?? "span"} tabIndex={0} content={props.text} class={`block truncate ${props.class ?? ""}`}>
      {props.text}
    </Tooltip>
  );
}
