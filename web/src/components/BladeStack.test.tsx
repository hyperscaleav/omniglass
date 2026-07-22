import { describe, it, expect, vi } from "vitest";
import { createSignal } from "solid-js";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import { createBladeController, useBladeEdit, type BladeDef } from "../lib/blades";
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

  it("offers Edit on an editable blade: pencil -> Save/Cancel, and Save runs the bound saver", async () => {
    const save = vi.fn(async () => {});
    const editRegistry: Record<string, BladeDef> = {
      thing: {
        Title: (p) => <>{`T:${p.id}`}</>,
        Body: (p) => {
          const e = useBladeEdit();
          e.bind({ save }); // binding makes the blade editable (a body with permission)
          return <div>{e.editing() ? `edit ${p.id}` : `read ${p.id}`}</div>;
        },
      },
    };
    const ctl = createBladeController();
    render(() => <BladeStack controller={ctl} registry={editRegistry} />);
    ctl.push({ kind: "thing", id: "a" });
    // Read mode: pencil present, body read-only, no Save yet.
    expect(screen.getByLabelText("Edit")).toBeTruthy();
    expect(screen.getByText("read a")).toBeTruthy();
    expect(screen.queryByText("Save")).toBeNull();
    // Enter edit: Save + Cancel appear, body flips to edit.
    fireEvent.click(screen.getByLabelText("Edit"));
    expect(screen.getByText("Save")).toBeTruthy();
    expect(screen.getByText("Cancel")).toBeTruthy();
    expect(screen.getByText("edit a")).toBeTruthy();
    // Save runs the bound saver and returns to read mode.
    fireEvent.click(screen.getByText("Save"));
    await waitFor(() => expect(save).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(screen.getByText("read a")).toBeTruthy());
  });

  it("does not offer Edit when the blade is not editable", () => {
    const ctl = createBladeController();
    render(() => <BladeStack controller={ctl} registry={registry} />);
    ctl.push({ kind: "user", id: "a" });
    expect(screen.queryByLabelText("Edit")).toBeNull();
  });

  // A create form hosted ON the blade stack (the new-interface blade) binds `primary`
  // and renders no buttons of its own, so the primary slot has to carry the gating a
  // submit button needs: disabled while the form is incomplete, spinning while in
  // flight. Without this the body would have to draw its own bar again.
  it("gates and spins a bound primary action", () => {
    const ctl = createBladeController();
    const [ready, setReady] = createSignal(false);
    const [busy, setBusy] = createSignal(false);
    const onClick = vi.fn();
    const creating: Record<string, BladeDef> = {
      create: {
        Title: () => <>New interface</>,
        Body: () => {
          useBladeEdit().bind({
            primary: () => ({ label: "Create interface", onClick, disabled: () => !ready(), busy }),
          });
          return <div>fields</div>;
        },
      },
    };
    render(() => <BladeStack controller={ctl} registry={creating} />);
    ctl.push({ kind: "create", id: "c1" });

    const btn = screen.getByText("Create interface").closest("button") as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
    fireEvent.click(btn);
    expect(onClick).not.toHaveBeenCalled();

    setReady(true);
    expect(btn.disabled).toBe(false);
    fireEvent.click(btn);
    expect(onClick).toHaveBeenCalledOnce();

    setBusy(true);
    expect(btn.disabled).toBe(true);
    expect(btn.querySelector(".loading-spinner")).toBeTruthy();
  });
});
