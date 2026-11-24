import { fireEvent, screen, within } from "@solidjs/testing-library";

// Delete lives in the Configure actions' ⋯ menu (#2439), whose panel portals to
// the body: open the menu inside `scope`, then press Delete wherever it landed.
export async function clickDelete(scope: HTMLElement = document.body) {
  fireEvent.click(await within(scope).findByRole("button", { name: "More actions" }));
  fireEvent.click(await screen.findByRole("button", { name: "Delete" }));
}
