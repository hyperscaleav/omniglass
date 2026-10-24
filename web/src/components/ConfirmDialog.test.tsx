import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import ConfirmDialog from "./ConfirmDialog";

// ConfirmDialog is the console's confirm for a decision that needs reading
// before it is made (#1078, trusting a changed key): a centred Kobalte
// AlertDialog, its body the host's, its rail drawn through PanelFooter. The
// safe choice holds the focus when it opens, so Enter never confirms by
// accident; Esc and Cancel close it and confirm nothing.

describe("ConfirmDialog", () => {
  function mount(over: Partial<Parameters<typeof ConfirmDialog>[0]> = {}) {
    const onConfirm = vi.fn();
    const onClose = vi.fn();
    render(() => (
      <ConfirmDialog open title="Trust the offered key?" confirmLabel="Trust this key" onConfirm={onConfirm} onClose={onClose} {...over}>
        <p>body</p>
      </ConfirmDialog>
    ));
    return { onConfirm, onClose };
  }

  it("is an alert dialog named by its title, its rail the one PanelFooter", () => {
    mount();
    const dialog = screen.getByRole("alertdialog", { name: "Trust the offered key?" });
    expect(dialog.textContent).toContain("body");
    const rail = dialog.querySelector("footer")!;
    expect(rail.className).toContain("border-t border-base-300");
    expect(rail.contains(screen.getByRole("button", { name: "Trust this key" }))).toBe(true);
  });

  it("opens with the focus on Cancel, the safe choice", async () => {
    mount();
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "Cancel" })));
  });

  it("confirms only on the confirm button", () => {
    const { onConfirm, onClose } = mount();
    fireEvent.click(screen.getByRole("button", { name: "Trust this key" }));
    expect(onConfirm).toHaveBeenCalledOnce();
    expect(onClose).not.toHaveBeenCalled();
  });

  it("closes on Cancel and on Escape, confirming nothing", () => {
    const { onConfirm, onClose } = mount();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(onClose).toHaveBeenCalledTimes(1);
    fireEvent.keyDown(screen.getByRole("alertdialog"), { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(2);
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("holds the confirm while busy or disabled, and says why it failed", () => {
    const { onConfirm } = mount({ disabled: true, error: "the component now offers SHA256:zzz" });
    const confirm = screen.getByRole("button", { name: "Trust this key" }) as HTMLButtonElement;
    expect(confirm.disabled).toBe(true);
    fireEvent.click(confirm);
    expect(onConfirm).not.toHaveBeenCalled();
    expect(screen.getByRole("alert").textContent).toMatch(/now offers/);
  });

  it("renders nothing while closed", () => {
    mount({ open: false });
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });
});
