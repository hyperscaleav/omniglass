import type { JSX } from "solid-js";
import Tooltip from "./Tooltip";

// Truncate: a value cut to one line that shows its full text on hover (#2486); it is not
// a tab stop, since the full text is in the DOM for a screen reader and the
// row or control around the cell is the focus stop. The one helper for every truncated value, so no cell
// leans on the native title attribute. The tip only exists when there is text.
export default function Truncate(props: { text: string | undefined; class?: string; as?: keyof JSX.IntrinsicElements }) {
  return (
    <Tooltip as={props.as ?? "span"} content={props.text} class={`block truncate ${props.class ?? ""}`}>
      {props.text}
    </Tooltip>
  );
}
