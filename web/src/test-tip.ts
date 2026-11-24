import { fireEvent, screen, waitFor } from "@solidjs/testing-library";
import { expect } from "vitest";

// tipOf reveals an element's Tooltip the way a keyboard user does (a Tab, then
// focus; the pointer too, for a trigger that takes no focus) and returns the
// tip's text, then dismisses it. A hover explanation is no longer a title
// attribute (#2486), so a test reads it from the shown tip.
export async function tipOf(el: Element | null | undefined): Promise<string> {
  if (!el) throw new Error("tipOf: no element");
  const node = el as HTMLElement;
  fireEvent.keyDown(document.body, { key: "Tab" });
  node.focus();
  fireEvent.pointerEnter(node, { pointerType: "mouse" });
  fireEvent.pointerMove(node, { pointerType: "mouse" });
  const tip = await screen.findByRole("tooltip");
  const text = tip.textContent ?? "";
  fireEvent.keyDown(node, { key: "Escape" });
  fireEvent.pointerLeave(node, { pointerType: "mouse" });
  node.blur();
  await waitFor(() => expect(screen.queryByRole("tooltip")).toBeNull());
  return text;
}

// isOff is whether a button is off: natively disabled, or aria-disabled (a
// titled Button stays focusable so its tip can say why it is off).
export const isOff = (el: Element | null | undefined): boolean =>
  !!el && ((el as HTMLButtonElement).disabled || el.getAttribute("aria-disabled") === "true");
