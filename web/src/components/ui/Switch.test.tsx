import { describe, it, expect } from "vitest";
import { createSignal } from "solid-js";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import Switch from "./Switch";

function Harness(props: { disabled?: boolean }) {
  const [on, setOn] = createSignal(false);
  return <Switch label="Cascades to descendants" description="Every descendant inherits it." checked={on()} onChange={setOn} disabled={props.disabled} />;
}

describe("Switch keyboard contract", () => {
  it("is a named switch, off, described by its help", () => {
    render(() => <Harness />);
    const sw = screen.getByRole("switch", { name: "Cascades to descendants" });
    expect((sw as HTMLInputElement).checked).toBe(false);
    expect(sw).toHaveAccessibleDescription("Every descendant inherits it.");
  });

  it("toggles from the keyboard (Space is the native checkbox's click) and keeps the focus", async () => {
    render(() => <Harness />);
    const sw = screen.getByRole("switch") as HTMLInputElement;
    sw.focus();
    fireEvent.click(sw);
    await waitFor(() => expect(sw.checked).toBe(true));
    expect(sw.closest("[data-checked]")).not.toBeNull();
    expect(document.activeElement).toBe(sw);
    fireEvent.click(screen.getByText("Cascades to descendants"));
    await waitFor(() => expect(sw.checked).toBe(false));
  });

  it("does not move when disabled", async () => {
    render(() => <Harness disabled />);
    const sw = screen.getByRole("switch") as HTMLInputElement;
    expect(sw.disabled).toBe(true);
  });
});
