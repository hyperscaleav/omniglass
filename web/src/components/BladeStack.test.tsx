import { describe, it, expect, vi } from "vitest";
import { createSignal } from "solid-js";
import { render, screen, fireEvent, waitFor } from "@solidjs/testing-library";
import { createBladeController, useBladeEdit, type BladeDef } from "../lib/blades";
import BladeStack from "./BladeStack";

// A fake two-kind registry: enough to prove the stack renders, drills, offsets,
// and dismisses. The bodies are inert; the controller drives everything.
const registry: Record<string, BladeDef> = {
  user: { Title: (p) => <>{`U:${p.id}`}</>, Body: (p) => <div>user body {p.id}</div> },
  group: { Title: (p) => <>{`G:${p.id}`}</>, Body: (p) => <div>group body {p.id}</div> },
};

// Blades portal to document.body (a page-tree `fixed` would resolve against
// any transformed ancestor, like the shell's filled fade-in), so queries go to
// the document, not the render container. The container arg is kept so call
// sites read unchanged where they pass one.
const asides = (_c?: HTMLElement) => document.querySelectorAll("aside[data-blade]");

describe("BladeStack", () => {
  it("renders nothing when the stack is empty, one blade per push, and offsets them", () => {
    const ctl = createBladeController();
    const { container } = render(() => <BladeStack controller={ctl} registry={registry} />);
    expect(asides(container).length).toBe(0);

    ctl.push({ kind: "user", id: "a" });
    expect(asides(container).length).toBe(1);
    expect(screen.getByText("U:a")).toBeTruthy();

    ctl.push({ kind: "group", id: "x" });
    expect(asides(container).length).toBe(2);
    expect(screen.getByText("G:x")).toBeTruthy();
    // The top blade (group) sits flush right; the covered one (user) is offset 40px.
    const [first, second] = [...asides(container)] as HTMLElement[];
    expect(second.style.right).toBe("0px");
    expect(first.style.right).toBe("40px");
  });

  it("Escape pops the top blade; the back button pops; close clears the stack", () => {
    const ctl = createBladeController();
    const { container } = render(() => <BladeStack controller={ctl} registry={registry} />);
    ctl.push({ kind: "user", id: "a" });
    ctl.push({ kind: "group", id: "x" });
    expect(asides(container).length).toBe(2);

    fireEvent.keyDown(window, { key: "Escape" });
    expect(asides(container).length).toBe(1);

    // Push again, then use the back button on the top blade.
    ctl.push({ kind: "group", id: "x" });
    expect(asides(container).length).toBe(2);
    fireEvent.click(screen.getAllByLabelText("Back")[0]);
    expect(asides(container).length).toBe(1);

    // Close clears everything.
    fireEvent.click(screen.getAllByLabelText("Close")[0]);
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
    expect(screen.queryByText("Delete")).toBeNull();
    expect(document.querySelector("aside[data-blade] footer")).toBeNull();
  });

  // The locked state, string form: a body that binds `locked` with one reason
  // string (the official row) gets BOTH footer buttons in their usual spots,
  // greyed, each wrapped in a tooltip carrying the reason. The live slots it
  // may also have bound are overridden, so neither button can act. The wrapper
  // span is the tab stop (a disabled button cannot take focus), so it carries
  // the reason as aria-label too: the AT path the CSS tooltip cannot provide.
  it("renders greyed Delete and Edit carrying the lock reason, overriding the live slots", () => {
    const save = vi.fn(async () => {});
    const del = vi.fn();
    const lockedRegistry: Record<string, BladeDef> = {
      thing: {
        Title: (p) => <>{`T:${p.id}`}</>,
        Body: () => {
          useBladeEdit().bind({
            save,
            destructive: () => ({ label: "Delete", tone: "danger", onClick: del }),
            locked: () => "Official: ships with Omniglass and updates with it.",
          });
          return <div>body</div>;
        },
      },
    };
    const ctl = createBladeController();
    render(() => <BladeStack controller={ctl} registry={lockedRegistry} />);
    ctl.push({ kind: "thing", id: "a" });

    const editBtn = screen.getByLabelText("Edit") as HTMLButtonElement;
    const deleteBtn = screen.getByText("Delete").closest("button") as HTMLButtonElement;
    expect(editBtn.disabled).toBe(true);
    expect(deleteBtn.disabled).toBe(true);
    for (const wrap of [editBtn.closest(".tooltip"), deleteBtn.closest(".tooltip")]) {
      expect(wrap?.getAttribute("data-tip")).toBe("Official: ships with Omniglass and updates with it.");
      // The keyboard/AT path: the focusable wrapper announces the reason.
      expect(wrap?.getAttribute("aria-label")).toBe("Official: ships with Omniglass and updates with it.");
      expect(wrap?.getAttribute("tabindex")).toBe("0");
    }
    // Neither button acts: no edit mode, no destructive call.
    fireEvent.click(editBtn);
    expect(screen.queryByText("Save")).toBeNull();
    fireEvent.click(deleteBtn);
    expect(del).not.toHaveBeenCalled();
    expect(save).not.toHaveBeenCalled();
  });

  // The locked state, object form: each side locks independently. An edit-only
  // lock greys the pencil side with its own reason while the bound destructive
  // stays live beside it (the delete-only caller's footer).
  it("an object lock greys only the side it names: live Delete beside a greyed Edit", () => {
    const del = vi.fn();
    const reg: Record<string, BladeDef> = {
      thing: {
        Title: () => <>T</>,
        Body: () => {
          useBladeEdit().bind({
            destructive: () => ({ label: "Delete", tone: "danger", onClick: del }),
            locked: () => ({ edit: "Requires vendor:update", delete: null }),
          });
          return <div>body</div>;
        },
      },
    };
    const ctl = createBladeController();
    render(() => <BladeStack controller={ctl} registry={reg} />);
    ctl.push({ kind: "thing", id: "a" });

    const editBtn = screen.getByLabelText("Edit") as HTMLButtonElement;
    expect(editBtn.disabled).toBe(true);
    expect(editBtn.closest(".tooltip")?.getAttribute("data-tip")).toBe("Requires vendor:update");
    expect(editBtn.closest(".tooltip")?.getAttribute("aria-label")).toBe("Requires vendor:update");
    const deleteBtn = screen.getByText("Delete").closest("button") as HTMLButtonElement;
    expect(deleteBtn.disabled).toBe(false);
    fireEvent.click(deleteBtn);
    expect(del).toHaveBeenCalledOnce();
  });

  // The mirror: a delete-only lock greys Delete with the delete reason (never
  // the update one) while the live pencil still enters edit mode.
  it("an object lock on delete greys Delete with its own reason while the pencil stays live", () => {
    const save = vi.fn(async () => {});
    const reg: Record<string, BladeDef> = {
      thing: {
        Title: () => <>T</>,
        Body: () => {
          const e = useBladeEdit();
          e.bind({ save, locked: () => ({ edit: null, delete: "Requires vendor:delete" }) });
          return <div>{e.editing() ? "editing" : "reading"}</div>;
        },
      },
    };
    const ctl = createBladeController();
    render(() => <BladeStack controller={ctl} registry={reg} />);
    ctl.push({ kind: "thing", id: "a" });

    const deleteBtn = screen.getByText("Delete").closest("button") as HTMLButtonElement;
    expect(deleteBtn.disabled).toBe(true);
    expect(deleteBtn.closest(".tooltip")?.getAttribute("data-tip")).toBe("Requires vendor:delete");
    expect(deleteBtn.closest(".tooltip")?.getAttribute("aria-label")).toBe("Requires vendor:delete");
    // The edit side is live: the pencil flips the body into edit mode.
    fireEvent.click(screen.getByLabelText("Edit"));
    expect(screen.getByText("Save")).toBeTruthy();
    expect(screen.getByText("editing")).toBeTruthy();
  });

  // The composition guard: `locked` belongs to detail blades and never beside
  // `primary` (a locked create blade renders a dead form). Dev builds warn once
  // with the blade context so the composition cannot land silently.
  it("warns once in dev when a body binds primary beside a non-null lock", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const reg: Record<string, BladeDef> = {
      create: {
        Title: () => <>New thing</>,
        Body: () => {
          useBladeEdit().bind({
            primary: () => ({ label: "Create thing", onClick: () => {} }),
            locked: () => "Official: ships with Omniglass and updates with it.",
          });
          return <div>fields</div>;
        },
      },
    };
    const ctl = createBladeController();
    render(() => <BladeStack controller={ctl} registry={reg} />);
    ctl.push({ kind: "create", id: "c1" });

    expect(warn).toHaveBeenCalledTimes(1);
    expect(String(warn.mock.calls[0][0])).toContain("create:c1");
    warn.mockRestore();
  });

  // A null lock is no lock: a body that binds only `locked` returning null has
  // registered nothing actionable, so the footer stays away entirely.
  it("treats a null lock as unlocked and renders no footer for it alone", () => {
    const nullLocked: Record<string, BladeDef> = {
      thing: {
        Title: () => <>T</>,
        Body: () => {
          useBladeEdit().bind({ locked: () => null });
          return <div>body</div>;
        },
      },
    };
    const ctl = createBladeController();
    render(() => <BladeStack controller={ctl} registry={nullLocked} />);
    ctl.push({ kind: "thing", id: "a" });
    expect(screen.queryByLabelText("Edit")).toBeNull();
    expect(screen.queryByText("Delete")).toBeNull();
    expect(document.querySelector("aside[data-blade] footer")).toBeNull();
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
