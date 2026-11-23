import { describe, it, expect } from "vitest";
import { createSignal } from "solid-js";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import Select from "./Select";

type Opt = { code: string; name: string; disabled?: boolean };
const options: Opt[] = [
  { code: "a", name: "Alpha" },
  { code: "b", name: "Bravo", disabled: true },
  { code: "c", name: "Charlie" },
];

function Harness(props: { onChange?: (id: string) => void }) {
  const [v, setV] = createSignal("a");
  return (
    <Select<Opt>
      options={options}
      optionValue="code"
      optionLabel="name"
      value={options.find((o) => o.code === v())}
      onChange={(o) => { setV(o.code); props.onChange?.(o.code); }}
      id="pick"
    />
  );
}

const trigger = () => screen.getByRole("button") as HTMLElement;

describe("Select keyboard contract", () => {
  it("shows the chosen label closed, and is the labelable field target", () => {
    render(() => <Harness />);
    expect(trigger().textContent).toBe("Alpha");
    expect(trigger().id).toBe("pick");
    expect(trigger().hasAttribute("data-field-target")).toBe(true);
  });

  it("opens on Enter, moves past a disabled option, selects on Enter, returns focus", async () => {
    const picked: string[] = [];
    render(() => <Harness onChange={(id) => picked.push(id)} />);
    trigger().focus();
    fireEvent.keyDown(trigger(), { key: "Enter" });
    const list = await screen.findByRole("listbox");
    // The focus moves onto the chosen option, and the arrows move it on.
    const focused = () => (list.contains(document.activeElement) ? document.activeElement!.textContent : null);
    await waitFor(() => expect(focused()).toBe("Alpha"));
    fireEvent.keyDown(document.activeElement!, { key: "ArrowDown" });
    await waitFor(() => expect(focused()).toBe("Charlie"));
    fireEvent.keyDown(document.activeElement!, { key: "Enter" });
    await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
    expect(picked).toEqual(["c"]);
    expect(trigger().textContent).toBe("Charlie");
    await waitFor(() => expect(document.activeElement).toBe(trigger()));
  });

  it("dismisses on Esc without changing the value", async () => {
    const picked: string[] = [];
    render(() => <Harness onChange={(id) => picked.push(id)} />);
    trigger().focus();
    fireEvent.keyDown(trigger(), { key: "Enter" });
    const list = await screen.findByRole("listbox");
    await waitFor(() => expect(list.contains(document.activeElement)).toBe(true));
    fireEvent.keyDown(document.activeElement!, { key: "ArrowDown" });
    fireEvent.keyDown(document.activeElement!, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
    expect(picked).toEqual([]);
    expect(trigger().textContent).toBe("Alpha");
    await waitFor(() => expect(document.activeElement).toBe(trigger()));
  });
});
