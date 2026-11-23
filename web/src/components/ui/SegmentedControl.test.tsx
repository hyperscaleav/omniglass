import { describe, it, expect } from "vitest";
import { createSignal } from "solid-js";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import SegmentedControl from "./SegmentedControl";

function Harness() {
  const [v, setV] = createSignal("scratch");
  return (
    <>
      <span data-testid="v">{v()}</span>
      <SegmentedControl
        label="How to start"
        value={v()}
        onChange={setV}
        options={[
          { value: "scratch", label: "From scratch" },
          { value: "template", label: "From a template" },
        ]}
      />
    </>
  );
}

const radio = (name: string) => screen.getByRole("radio", { name }) as HTMLInputElement;

describe("SegmentedControl keyboard contract", () => {
  it("is a named radio group with the chosen segment checked and drawn active", () => {
    render(() => <Harness />);
    expect(screen.getByRole("radiogroup", { name: "How to start" })).toBeTruthy();
    expect(radio("From scratch").checked).toBe(true);
    expect(radio("From scratch").closest(".tab")!.classList.contains("tab-active")).toBe(true);
    expect(radio("From a template").checked).toBe(false);
  });

  // The segments are one native radio group (a shared name), which is what
  // moves and chooses on the arrow keys; jsdom does not run the browser's
  // radio-group arrows, so that half is driven live in the probe (#2485 PR).
  it("is one radio group by name, the focus on the chosen segment, and picks on click", async () => {
    render(() => <Harness />);
    expect(radio("From scratch").name).not.toBe("");
    expect(radio("From scratch").name).toBe(radio("From a template").name);
    expect(radio("From scratch").tabIndex).not.toBe(-1);
    fireEvent.click(screen.getByText("From a template"));
    await waitFor(() => expect(screen.getByTestId("v").textContent).toBe("template"));
    await waitFor(() => expect(radio("From a template").checked).toBe(true));
    expect(radio("From a template").closest(".tab")!.classList.contains("tab-active")).toBe(true);
  });
});
