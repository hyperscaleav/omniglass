import { describe, it, expect } from "vitest";
import { createSignal } from "solid-js";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import Collapsible from "./Collapsible";

describe("Collapsible keyboard contract", () => {
  it("is a button that reports and toggles its section, the focus staying on it", async () => {
    render(() => <Collapsible title="Permissions (3)"><p>alarm:read</p></Collapsible>);
    const trigger = screen.getByRole("button", { name: "Permissions (3)" });
    expect(trigger.getAttribute("aria-expanded")).toBe("false");
    expect(screen.queryByText("alarm:read")).toBeNull();
    trigger.focus();
    fireEvent.click(trigger); // Enter and Space on a <button> are a click
    await waitFor(() => expect(screen.getByText("alarm:read")).toBeTruthy());
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    expect(trigger.getAttribute("aria-controls")).toBe(screen.getByText("alarm:read").parentElement!.id);
    expect(document.activeElement).toBe(trigger);
    fireEvent.click(trigger);
    await waitFor(() => expect(screen.queryByText("alarm:read")).toBeNull());
    expect(document.activeElement).toBe(trigger);
  });

  it("follows a controlled open state, and starts open with defaultOpen", async () => {
    const [open, setOpen] = createSignal(false);
    render(() => (
      <>
        <Collapsible title="Controlled" open={open()} onOpenChange={setOpen}><p>inside</p></Collapsible>
        <Collapsible title="Starts open" defaultOpen><p>already</p></Collapsible>
      </>
    ));
    expect(screen.getByText("already")).toBeTruthy();
    setOpen(true);
    await waitFor(() => expect(screen.getByText("inside")).toBeTruthy());
    fireEvent.click(screen.getByRole("button", { name: "Controlled" }));
    await waitFor(() => expect(open()).toBe(false));
  });
});
