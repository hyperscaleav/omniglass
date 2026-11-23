import { fireEvent, screen, waitFor, within } from "@solidjs/testing-library";

// Drivers for a Picker in a test, the way a keyboard operator uses it: type
// into the field, then choose from the portalled list (read from the document,
// not the render container).

// Type text into the picker's field; returns the open list's option texts.
export async function typeInPicker(input: HTMLElement, text: string): Promise<string[]> {
  input.focus();
  fireEvent.input(input, { target: { value: text } });
  return within(await screen.findByRole("listbox")).getAllByRole("option").map((o) => o.textContent ?? "");
}

// Type the label and choose the option whose label is exactly it.
export async function pickInPicker(input: HTMLElement, label: string): Promise<void> {
  await typeInPicker(input, label);
  const list = await screen.findByRole("listbox");
  const hit = await waitFor(() => {
    const o = within(list).getAllByRole("option").find((el) => el.querySelector("[id$=label]")?.textContent === label || el.firstElementChild?.textContent === label);
    if (!o) throw new Error(`no picker option ${label}`);
    return o;
  });
  fireEvent.click(hit);
}
