import { describe, it, expect } from "vitest";
import { createSignal } from "solid-js";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import Picker, { type PickerOption } from "./Picker";
import Drawer from "../Drawer";

const ROOMS: PickerOption[] = [
  { value: "r1", label: "Boardroom", detail: "HQ / Floor 2" },
  { value: "r2", label: "Huddle 4", detail: "HQ / Floor 3" },
  { value: "r3", label: "Training", detail: "East campus" },
  { value: "r4", label: "Lobby", detail: "HQ", disabled: true },
];

function Single(props: { onCreate?: (t: string) => void; initial?: string | null; options?: PickerOption[] }) {
  const [v, setV] = createSignal<string | null>(props.initial ?? null);
  return (
    <>
      <span data-testid="value">{v() ?? "(none)"}</span>
      <Picker aria-label="Room" options={props.options ?? ROOMS} value={v()} onChange={setV} onCreate={props.onCreate} placeholder="Choose a room" />
    </>
  );
}

const input = () => screen.getByRole("combobox", { name: "Room" }) as HTMLInputElement;
const listbox = () => screen.findByRole("listbox");
const optionTexts = async () => within(await listbox()).getAllByRole("option").map((o) => o.textContent);
const highlighted = () => document.querySelector("[role=option][data-highlighted]")?.textContent ?? null;
const type = (text: string) => {
  input().focus();
  fireEvent.input(input(), { target: { value: text } });
};

describe("Picker keyboard contract", () => {
  it("filters as you type, matching the secondary text too", async () => {
    render(() => <Single />);
    type("floor");
    expect(await optionTexts()).toEqual(["BoardroomHQ / Floor 2", "Huddle 4HQ / Floor 3"]);
    type("train");
    await waitFor(async () => expect(await optionTexts()).toEqual(["TrainingEast campus"]));
  });

  it("opens on ArrowDown, moves with the arrows, selects on Enter and keeps the focus in the field", async () => {
    render(() => <Single />);
    input().focus();
    fireEvent.keyDown(input(), { key: "ArrowDown" });
    await listbox();
    await waitFor(() => expect(highlighted()).toContain("Boardroom"));
    fireEvent.keyDown(input(), { key: "ArrowDown" });
    await waitFor(() => expect(highlighted()).toContain("Huddle 4"));
    fireEvent.keyDown(input(), { key: "Enter" });
    await waitFor(() => expect(screen.getByTestId("value").textContent).toBe("r2"));
    await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
    expect(input().value).toBe("Huddle 4");
    expect(document.activeElement).toBe(input());
  });

  it("draws a disabled option but does not select it", async () => {
    render(() => <Single />);
    type("lobby");
    const opt = within(await listbox()).getByRole("option");
    expect(opt.getAttribute("aria-disabled")).toBe("true");
    fireEvent.click(opt);
    expect(screen.getByTestId("value").textContent).toBe("(none)");
  });

  it("dismisses on Esc without choosing, the focus left in the field", async () => {
    render(() => <Single initial="r1" />);
    input().focus();
    fireEvent.keyDown(input(), { key: "ArrowDown" });
    await listbox();
    fireEvent.keyDown(input(), { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("listbox")).toBeNull());
    expect(screen.getByTestId("value").textContent).toBe("r1");
    expect(document.activeElement).toBe(input());
  });

  it("opens from its button and hands the focus back to the field", async () => {
    render(() => <Single />);
    const button = screen.getByRole("button", { name: "Show suggestions" });
    fireEvent.pointerDown(button, { pointerType: "mouse" });
    fireEvent.click(button);
    expect((await optionTexts()).length).toBe(4);
    await waitFor(() => expect(document.activeElement).toBe(input()));
  });

  it("offers to create what the typing names when nothing matches it exactly, and selects nothing itself", async () => {
    const created: string[] = [];
    render(() => <Single onCreate={(t) => created.push(t)} />);
    type("Board");
    // A partial match keeps the create row: "Board" is not "Boardroom".
    expect(await optionTexts()).toEqual(["BoardroomHQ / Floor 2", "Create “Board”"]);
    type("  Studio B ");
    await waitFor(async () => expect(await optionTexts()).toEqual(["Create “Studio B”"]));
    fireEvent.keyDown(input(), { key: "ArrowDown" });
    fireEvent.keyDown(input(), { key: "Enter" });
    await waitFor(() => expect(created).toEqual(["Studio B"]));
    expect(screen.getByTestId("value").textContent).toBe("(none)");
    // An exact name (any case) has no create row.
    type("boardroom");
    await waitFor(async () => expect(await optionTexts()).toEqual(["BoardroomHQ / Floor 2"]));
  });

  it("says so when nothing matches and nothing can be created", async () => {
    render(() => <Single />);
    type("zzz");
    expect(await screen.findByText("No matches")).toBeTruthy();
  });
});

describe("Picker multiple", () => {
  function Multi() {
    const [v, setV] = createSignal<string[]>(["r1"]);
    return (
      <>
        <span data-testid="value">{v().join(",")}</span>
        <Picker multiple aria-label="Rooms" options={ROOMS} value={v()} onChange={setV} />
      </>
    );
  }
  const minput = () => screen.getByRole("combobox", { name: "Rooms" }) as HTMLInputElement;

  it("shows each choice as a removable chip and adds more from the keyboard, staying open", async () => {
    render(() => <Multi />);
    expect(screen.getByRole("button", { name: "Remove Boardroom" })).toBeTruthy();
    minput().focus();
    fireEvent.input(minput(), { target: { value: "train" } });
    await screen.findByRole("listbox");
    fireEvent.keyDown(minput(), { key: "ArrowDown" });
    fireEvent.keyDown(minput(), { key: "Enter" });
    await waitFor(() => expect(screen.getByTestId("value").textContent).toBe("r1,r3"));
    expect(screen.getByRole("button", { name: "Remove Training" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Remove Boardroom" }));
    await waitFor(() => expect(screen.getByTestId("value").textContent).toBe("r3"));
  });

  it("removes the last choice with Backspace in an empty field", async () => {
    render(() => <Multi />);
    minput().focus();
    fireEvent.keyDown(minput(), { key: "Backspace" });
    await waitFor(() => expect(screen.getByTestId("value").textContent).toBe(""));
  });
});

// The portal rule: a drawer is a modal dialog that hides whatever is added to
// the page outside it, so a list portalled to the body must still be reachable
// and choosable from inside one.
describe("Picker in a drawer", () => {
  it("opens a reachable list and picks from it", async () => {
    render(() => (
      <Drawer open onClose={() => {}} title="Place component">
        <Single />
      </Drawer>
    ));
    type("hud");
    const list = await listbox();
    expect(list.closest("[aria-hidden=true]")).toBeNull();
    fireEvent.click(within(list).getByRole("option"));
    await waitFor(() => expect(screen.getByTestId("value").textContent).toBe("r2"));
    expect(screen.getByRole("dialog")).toBeTruthy();
  });
});
