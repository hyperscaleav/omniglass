import { describe, it, expect, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import { Toaster, clearToasts, dismissToast, toast, toasts, MAX_TOASTS } from "./Toast";

// The toast (#1155, #2485): any surface raises a notice, the shell's one
// Toaster draws it bottom right, and it goes away by itself or when dismissed.
afterEach(() => clearToasts());


describe("Toast", () => {
  it("draws a raised toast in the notifications region, its title and live body read as a status", async () => {
    const { container } = render(() => <Toaster />);
    toast.success("Setting saved", { body: () => <span>monitoring.cadence is 30s</span> });
    const card = await screen.findByTestId("toast");
    expect(card.getAttribute("role")).toBe("status");
    expect(card).toHaveAccessibleName("Setting saved");
    expect(card.textContent).toContain("monitoring.cadence is 30s");
    expect(card.getAttribute("data-tone")).toBe("success");
    expect(screen.getByRole("region").contains(card)).toBe(true);
    expect(container.contains(card)).toBe(false);
  });

  it("moves the focus to the region on Alt+T, and Esc closes the focused toast", async () => {
    render(() => (
      <>
        <button>Save</button>
        <Toaster />
      </>
    ));
    const save = screen.getByText("Save");
    save.focus();
    toast.error("Could not save the setting", { ttlMs: 0 });
    const card = await screen.findByTestId("toast");
    fireEvent.keyDown(document, { altKey: true, code: "KeyT", key: "t" });
    await waitFor(() => expect(screen.getByRole("region").contains(document.activeElement)).toBe(true));
    card.focus();
    fireEvent.keyDown(card, { key: "Escape" });
    await waitFor(() => expect(screen.queryByTestId("toast")).toBeNull());
    expect(toasts()).toEqual([]);
  });

  it("closes from its dismiss button and runs its action once, closing it", async () => {
    render(() => <Toaster />);
    const opened: string[] = [];
    toast({ tone: "info", title: "set-volume issued", ttlMs: 0, action: { label: "Open", onClick: () => opened.push("row") } });
    toast.info("second", { ttlMs: 0 });
    await waitFor(() => expect(screen.getAllByTestId("toast")).toHaveLength(2));
    fireEvent.click(screen.getByRole("button", { name: "Open" }));
    expect(opened).toEqual(["row"]);
    await waitFor(() => expect(screen.getAllByTestId("toast")).toHaveLength(1));
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    await waitFor(() => expect(screen.queryByTestId("toast")).toBeNull());
  });

  it("goes away by itself after its time, and a ttl of 0 stays", async () => {
    render(() => <Toaster />);
    toast.info("brief", { ttlMs: 60 });
    toast.info("sticky", { ttlMs: 0 });
    await waitFor(() => expect(screen.getAllByTestId("toast")).toHaveLength(2));
    await waitFor(() => expect(toasts().map((t) => t.title)).toEqual(["sticky"]), { timeout: 3000 });
    expect(screen.getAllByTestId("toast")).toHaveLength(1);
  });

  it("shows at most MAX_TOASTS and queues a burst behind them, losing none", async () => {
    render(() => <Toaster />);
    const ids = Array.from({ length: MAX_TOASTS + 2 }, (_, i) => toast.info(`t${i}`, { ttlMs: 0 }));
    await waitFor(() => expect(screen.getAllByTestId("toast")).toHaveLength(MAX_TOASTS));
    expect(toasts()).toHaveLength(MAX_TOASTS + 2);
    dismissToast(ids[0]);
    await waitFor(() => expect(screen.getAllByText(`t${MAX_TOASTS}`)).toHaveLength(1));
  });
});
