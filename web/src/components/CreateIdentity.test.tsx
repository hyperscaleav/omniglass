import { describe, it, expect } from "vitest";
import { render, screen, fireEvent } from "@solidjs/testing-library";
import { createSignal } from "solid-js";
import CreateIdentity from "./CreateIdentity";
import { type NameMint } from "../lib/namegen";

// The shared Identity section of the three estate create forms. What is asserted
// here is the CONTRACT the three pages share; each page's own test proves it is
// wired to this rather than to a copy of it.
function mount(mint: NameMint | null, bucket = "at HQ / Boardroom") {
  const [name, setName] = createSignal("");
  const [display, setDisplay] = createSignal("");
  const r = render(() => (
    <CreateIdentity
      kind="component"
      mint={() => mint}
      bucket={() => bucket}
      name={name}
      setName={setName}
      display={display}
      setDisplay={setDisplay}
      namePlaceholder="mic-2"
      displayPlaceholder="Ceiling Mic 2"
    />
  ));
  return {
    ...r,
    name: screen.getByPlaceholderText("mic-2") as HTMLInputElement,
    display: screen.getByPlaceholderText("Ceiling Mic 2") as HTMLInputElement,
    readName: name,
    readDisplay: display,
  };
}

describe("CreateIdentity", () => {
  it("shows the shape the platform will mint, with the ordinal as a token", () => {
    mount({ stem: "display", bareFirst: false });
    expect(screen.getByText("display-n")).toBeTruthy();
    // And never a number, which is the value that cannot be known before the
    // row exists.
    expect(screen.queryByText("display-1")).toBeNull();
  });

  it("shows the placement bucket the name has to be unique in, as text and not as a field", () => {
    mount({ stem: "display", bareFirst: false }, "at HQ / West / Boardroom");
    const scope = screen.getByText(/Unique at HQ \/ West \/ Boardroom/);
    expect(scope).toBeTruthy();
    // Context, never part of the name: nothing put the path into the input.
    expect((screen.getByPlaceholderText("mic-2") as HTMLInputElement).value).toBe("");
    expect(scope.querySelector("input")).toBeNull();
  });

  it("says a suppressed first ordinal reappears, so the shown value cannot silently differ", () => {
    mount({ stem: "boardroom", bareFirst: true });
    expect(screen.getByText("boardroom")).toBeTruthy();
    expect(screen.getByText(/boardroom-2/)).toBeTruthy();
  });

  it("names the missing fact instead of a preview when nothing will generate", () => {
    mount(null);
    expect(screen.queryByText(/Generated name/)).toBeNull();
    expect(screen.getByText(/Choose a product/)).toBeTruthy();
  });

  it("says the typed name has taken over, and how to hand it back", () => {
    const { name } = mount({ stem: "display", bareFirst: false });
    fireEvent.input(name, { target: { value: "front-mic" } });
    expect(screen.getByText(/You are naming this yourself/)).toBeTruthy();
    expect(screen.getByText(/naming this yourself/).textContent).toContain("display-n");
  });

  it("never rewrites the name from the display name", () => {
    // The coupling lib/entities.ts's createIdentity owns for the registry
    // pages, which is exactly wrong here: a blank name is the request to
    // GENERATE one, so filling it from a typed label would claim the pen on the
    // operator's behalf the moment they typed a word.
    const { display, readName } = mount({ stem: "display", bareFirst: false });
    fireEvent.input(display, { target: { value: "Front Ceiling Mic" } });
    expect(readName()).toBe("");
    expect(screen.getByText("display-n")).toBeTruthy();
  });

  it("leaves the display name alone when the name is overridden", () => {
    const { name, display, readDisplay } = mount({ stem: "display", bareFirst: false });
    fireEvent.input(display, { target: { value: "Front Ceiling Mic" } });
    fireEvent.input(name, { target: { value: "front-mic" } });
    expect(readDisplay()).toBe("Front Ceiling Mic");
    expect(display.value).toBe("Front Ceiling Mic");
  });
});
