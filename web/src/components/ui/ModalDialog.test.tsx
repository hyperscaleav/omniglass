import { describe, it, expect } from "vitest";
import { createSignal } from "solid-js";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import ModalDialog, { DialogClose, DialogHeading } from "./ModalDialog";
// Imported so the alert-dialog module is loaded in this file, as it is in the
// console's bundle: the role regression below only shows with both present.
import ConfirmDialog from "./ConfirmDialog";

function Harness(props: { alert?: boolean; initialFocus?: (p: HTMLElement) => HTMLElement | null }) {
  const [open, setOpen] = createSignal(false);
  return (
    <div style={{ overflow: "hidden" }} data-testid="clip">
      <button onClick={() => setOpen(true)}>Open</button>
      <ModalDialog open={open()} onClose={() => setOpen(false)} alert={props.alert} initialFocus={props.initialFocus} class="panel">
        <DialogHeading>Rename outpost</DialogHeading>
        <input aria-label="Name" />
        <button>Second</button>
        <DialogClose aria-label="Close">x</DialogClose>
      </ModalDialog>
    </div>
  );
}

const openIt = async () => {
  const trigger = screen.getByText("Open");
  trigger.focus();
  fireEvent.click(trigger);
  const panel = await waitFor(() => {
    const el = document.querySelector<HTMLElement>("[role=dialog],[role=alertdialog]");
    if (!el) throw new Error("no dialog open");
    return el;
  });
  return { trigger, panel };
};

describe("ModalDialog keyboard contract", () => {
  it("opens named by its title, portalled, with the focus inside", async () => {
    const { container } = render(() => <Harness />);
    const { panel } = await openIt();
    expect(panel.getAttribute("role")).toBe("dialog");
    expect(panel).toHaveAccessibleName("Rename outpost");
    expect(container.querySelector("[data-testid=clip]")!.contains(panel)).toBe(false);
    await waitFor(() => expect(panel.contains(document.activeElement)).toBe(true));
  });

  it("closes on Esc and hands the focus back to the trigger", async () => {
    render(() => <Harness />);
    const { trigger, panel } = await openIt();
    fireEvent.keyDown(panel, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await waitFor(() => expect(document.activeElement).toBe(trigger));
  });

  it("closes from its close control", async () => {
    render(() => <Harness />);
    await openIt();
    fireEvent.click(screen.getByLabelText("Close"));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  // Kobalte assigns Dialog's and AlertDialog's parts onto one shared root, so
  // reading `Dialog.Content` returned the alert content once both modules were
  // loaded: every Drawer announced itself as an alert. The wrapper takes the
  // named exports, so each role is what it says.
  it("is a dialog unless marked alert, with both modules loaded", async () => {
    expect(ConfirmDialog).toBeTypeOf("function");
    render(() => <Harness />);
    expect((await openIt()).panel.getAttribute("role")).toBe("dialog");
  });

  it("is an alertdialog when marked alert, the focus where initialFocus puts it", async () => {
    render(() => <Harness alert initialFocus={(p) => p.querySelector<HTMLElement>("button")} />);
    const { panel } = await openIt();
    expect(panel.getAttribute("role")).toBe("alertdialog");
    await waitFor(() => expect(document.activeElement).toBe(screen.getByText("Second")));
  });
});
