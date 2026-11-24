import { describe, it, expect } from "vitest";
import { createSignal } from "solid-js";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import Tabs from "./Tabs";

function Harness() {
  const [v, setV] = createSignal("all");
  return (
    <>
      <button>before</button>
      <Tabs
        label="Permissions"
        value={v()}
        onChange={setV}
        tabs={[
          { value: "all", label: "All" },
          { value: "held", label: "Held" },
          { value: "missing", label: "Missing", disabled: true },
        ]}
      >
        {(value) => <p>panel {value}</p>}
      </Tabs>
    </>
  );
}

const tab = (name: string) => screen.getByRole("tab", { name });

describe("Tabs keyboard contract", () => {
  it("is a named tab list whose active tab controls the one mounted panel", () => {
    render(() => <Harness />);
    expect(screen.getByRole("tablist", { name: "Permissions" })).toBeTruthy();
    expect(tab("All").getAttribute("aria-selected")).toBe("true");
    const panel = screen.getByRole("tabpanel");
    expect(panel.textContent).toBe("panel all");
    expect(tab("All").getAttribute("aria-controls")).toBe(panel.id);
    expect(tab("All").classList.contains("tab-active")).toBe(true);
  });

  it("moves and activates with the arrows, skipping a disabled tab, and Home/End jump", async () => {
    render(() => <Harness />);
    tab("All").focus();
    fireEvent.keyDown(tab("All"), { key: "ArrowRight" });
    await waitFor(() => expect(document.activeElement).toBe(tab("Held")));
    await waitFor(() => expect(screen.getByRole("tabpanel").textContent).toBe("panel held"));
    fireEvent.keyDown(tab("Held"), { key: "ArrowRight" });
    await waitFor(() => expect(document.activeElement).toBe(tab("All")));
    fireEvent.keyDown(tab("All"), { key: "End" });
    await waitFor(() => expect(document.activeElement).toBe(tab("Held")));
    fireEvent.keyDown(tab("Held"), { key: "Home" });
    await waitFor(() => expect(document.activeElement).toBe(tab("All")));
  });

  it("selects on click, and only the active tab is a tab stop", async () => {
    render(() => <Harness />);
    fireEvent.click(tab("Held"));
    await waitFor(() => expect(tab("Held").getAttribute("aria-selected")).toBe("true"));
    expect(tab("Held").tabIndex).toBe(0);
    expect(tab("All").tabIndex).toBe(-1);
  });
});
