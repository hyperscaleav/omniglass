import { describe, it, expect } from "vitest";
import { createSignal } from "solid-js";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import Popover from "./Popover";

function Menu(props: { onPick: (v: string) => void }) {
  const [open, setOpen] = createSignal(false);
  return (
    <div data-testid="clip" style={{ overflow: "hidden" }}>
      <Popover
        open={open()}
        onOpenChange={setOpen}
        aria-label="Snooze"
        contentLabel="Snooze for"
        content={
          <>
            <button onClick={() => { setOpen(false); props.onPick("1h"); }}>1 hour</button>
            <button onClick={() => { setOpen(false); props.onPick("1d"); }}>1 day</button>
          </>
        }
      >
        Snooze
      </Popover>
    </div>
  );
}

describe("Popover keyboard contract", () => {
  it("opens from its button with the focus moved into the panel, portalled", async () => {
    const { container } = render(() => <Menu onPick={() => {}} />);
    const trigger = screen.getByRole("button", { name: "Snooze" });
    expect(trigger.getAttribute("aria-haspopup")).toBe("dialog");
    fireEvent.click(trigger);
    const panel = await screen.findByRole("dialog", { name: "Snooze for" });
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    expect(container.querySelector("[data-testid=clip]")!.contains(panel)).toBe(false);
    await waitFor(() => expect(panel.contains(document.activeElement)).toBe(true));
  });

  it("closes on Esc and returns the focus to the trigger", async () => {
    render(() => <Menu onPick={() => {}} />);
    const trigger = screen.getByRole("button", { name: "Snooze" });
    trigger.focus();
    fireEvent.click(trigger);
    const panel = await screen.findByRole("dialog");
    fireEvent.keyDown(panel, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await waitFor(() => expect(document.activeElement).toBe(trigger));
  });

  it("lets a choice in the panel act and close it", async () => {
    const picked: string[] = [];
    render(() => <Menu onPick={(v) => picked.push(v)} />);
    fireEvent.click(screen.getByRole("button", { name: "Snooze" }));
    fireEvent.click(await screen.findByText("1 day"));
    expect(picked).toEqual(["1d"]);
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });
});
