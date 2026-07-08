import { describe, it, expect } from "vitest";
import { render, fireEvent } from "@solidjs/testing-library";
import { createBladeController, type BladeDef } from "../lib/blades";
import BladeStack from "./BladeStack";

// A fake two-kind registry: enough to prove the stack renders, drills, offsets,
// and dismisses. The bodies are inert; the controller drives everything.
const registry: Record<string, BladeDef> = {
  user: { Title: (p) => <>{`U:${p.id}`}</>, Body: (p) => <div>user body {p.id}</div> },
  group: { Title: (p) => <>{`G:${p.id}`}</>, Body: (p) => <div>group body {p.id}</div> },
};

const asides = (c: HTMLElement) => c.querySelectorAll("aside[data-blade]");

describe("BladeStack", () => {
  it("renders nothing when the stack is empty, one blade per push, and offsets them", () => {
    const ctl = createBladeController();
    const { container, getByText } = render(() => <BladeStack controller={ctl} registry={registry} />);
    expect(asides(container).length).toBe(0);

    ctl.push({ kind: "user", id: "a" });
    expect(asides(container).length).toBe(1);
    expect(getByText("U:a")).toBeTruthy();

    ctl.push({ kind: "group", id: "x" });
    expect(asides(container).length).toBe(2);
    expect(getByText("G:x")).toBeTruthy();
    // The top blade (group) sits flush right; the covered one (user) is offset 40px.
    const [first, second] = [...asides(container)] as HTMLElement[];
    expect(second.style.right).toBe("0px");
    expect(first.style.right).toBe("40px");
  });

  it("Escape pops the top blade; the back button pops; close clears the stack", () => {
    const ctl = createBladeController();
    const { container, getAllByLabelText } = render(() => <BladeStack controller={ctl} registry={registry} />);
    ctl.push({ kind: "user", id: "a" });
    ctl.push({ kind: "group", id: "x" });
    expect(asides(container).length).toBe(2);

    fireEvent.keyDown(window, { key: "Escape" });
    expect(asides(container).length).toBe(1);

    // Push again, then use the back button on the top blade.
    ctl.push({ kind: "group", id: "x" });
    expect(asides(container).length).toBe(2);
    fireEvent.click(getAllByLabelText("Back")[0]);
    expect(asides(container).length).toBe(1);

    // Close clears everything.
    fireEvent.click(getAllByLabelText("Close")[0]);
    expect(asides(container).length).toBe(0);
  });

  it("ignores a ref whose kind is not in the registry", () => {
    const ctl = createBladeController();
    const { container } = render(() => <BladeStack controller={ctl} registry={registry} />);
    ctl.push({ kind: "nope", id: "z" });
    expect(asides(container).length).toBe(0);
  });
});
