import { fireEvent, screen, within } from "@solidjs/testing-library";

// Drivers for a TreeSelect in a test. Its trigger is a button and its list is a
// portaled listbox, so a test opens it the way a keyboard operator does and reads
// the options from the document, not from the rendered container.

// The text of the trigger: the selected label, or the root option's label.
export const treeValue = (trigger: HTMLElement): string => trigger.textContent ?? "";

// Open the picker and return its option elements in list order. Scoped to the
// open listbox: a native <select> elsewhere on the page also has role=option.
export async function openTree(trigger: HTMLElement): Promise<HTMLElement[]> {
  fireEvent.keyDown(trigger, { key: "Enter" });
  return within(await screen.findByRole("listbox")).getAllByRole("option");
}

// The text of each option, in list order.
export async function treeOptions(trigger: HTMLElement): Promise<string[]> {
  return (await openTree(trigger)).map((o) => o.textContent ?? "");
}

// Open the picker and choose an option: by exact label, by pattern, or by its
// position in the list (for options whose labels are the same).
export async function pickTree(trigger: HTMLElement, which: string | RegExp | number): Promise<void> {
  const options = await openTree(trigger);
  const hit =
    typeof which === "number"
      ? options[which]
      : options.find((o) => (typeof which === "string" ? o.textContent === which : which.test(o.textContent ?? "")));
  if (!hit) throw new Error(`no tree option ${String(which)}; offered: ${options.map((o) => o.textContent).join(", ")}`);
  fireEvent.click(hit);
}

// Which option positions are selected, read from a freshly opened list.
export async function selectedTree(trigger: HTMLElement): Promise<number[]> {
  const options = await openTree(trigger);
  const at = options.flatMap((o, i) => (o.getAttribute("aria-selected") === "true" ? [i] : []));
  fireEvent.keyDown(trigger, { key: "Escape" });
  return at;
}
