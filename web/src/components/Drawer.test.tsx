import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, screen } from "@solidjs/testing-library";
import { createSignal, Show } from "solid-js";
import Drawer from "./Drawer";
import { useFormActions } from "../lib/formactions";

// The Drawer OWNS its action rail. A body registers what the buttons do and the
// shell draws them, the same contract BladeStack uses for the blade footer. These
// tests pin that ownership: a body that renders no footer JSX still gets a pinned,
// correctly styled rail, and a body that binds nothing gets no rail at all.

const footer = () => document.querySelector("footer");

describe("Drawer action rail", () => {
  it("renders no rail when the body binds nothing", () => {
    render(() => (
      <Drawer open={true} onClose={() => {}} title="New thing">
        <div>just fields</div>
      </Drawer>
    ));
    expect(footer()).toBeNull();
  });

  it("renders the bound submit button, and the body supplies no footer markup", () => {
    const submit = vi.fn();
    const Body = () => {
      useFormActions().bind({ submitLabel: "Create user", submit });
      return <div>fields only</div>;
    };
    render(() => (
      <Drawer open={true} onClose={() => {}} title="New user">
        <Body />
      </Drawer>
    ));
    const bar = footer()!;
    expect(bar).toBeTruthy();
    const btn = screen.getByRole("button", { name: "Create user" });
    expect(bar.contains(btn)).toBe(true);
    fireEvent.click(btn);
    expect(submit).toHaveBeenCalledOnce();
  });

  it("renders Cancel only when the body binds one", () => {
    const cancel = vi.fn();
    const Body = (p: { withCancel: boolean }) => {
      useFormActions().bind({
        submitLabel: "Create group",
        submit: () => {},
        ...(p.withCancel ? { cancel } : {}),
      });
      return <div>fields</div>;
    };
    const { unmount } = render(() => (
      <Drawer open={true} onClose={() => {}} title="New group">
        <Body withCancel={false} />
      </Drawer>
    ));
    expect(screen.queryByRole("button", { name: "Cancel" })).toBeNull();
    unmount();

    render(() => (
      <Drawer open={true} onClose={() => {}} title="New group">
        <Body withCancel={true} />
      </Drawer>
    ));
    const btn = screen.getByRole("button", { name: "Cancel" });
    fireEvent.click(btn);
    expect(cancel).toHaveBeenCalledOnce();
  });

  it("disables submit when the body reports invalid, and spins while busy", () => {
    const [busy, setBusy] = createSignal(false);
    const [ok, setOk] = createSignal(false);
    const Body = () => {
      useFormActions().bind({
        submitLabel: "Create tag key",
        submit: () => {},
        busy,
        disabled: () => !ok(),
      });
      return <div>fields</div>;
    };
    render(() => (
      <Drawer open={true} onClose={() => {}} title="New tag key">
        <Body />
      </Drawer>
    ));
    const btn = screen.getByRole("button", { name: "Create tag key" }) as HTMLButtonElement;
    expect(btn.disabled).toBe(true);
    setOk(true);
    expect(btn.disabled).toBe(false);
    setBusy(true);
    expect(btn.disabled).toBe(true);
    expect(btn.querySelector(".loading-spinner")).toBeTruthy();
  });

  // Profile's token Drawer swaps a create form for a reveal panel; each branch
  // binds its own action. The rail must follow the live branch.
  it("follows a body that swaps which branch is mounted", () => {
    const [done, setDone] = createSignal(false);
    const Form = () => {
      useFormActions().bind({ submitLabel: "Create token", submit: () => setDone(true) });
      return <div>form</div>;
    };
    const Reveal = () => {
      useFormActions().bind({ submitLabel: "Done", submit: () => {} });
      return <div>token</div>;
    };
    render(() => (
      <Drawer open={true} onClose={() => {}} title="Create API token">
        <Show when={done()} fallback={<Form />}>
          <Reveal />
        </Show>
      </Drawer>
    ));
    fireEvent.click(screen.getByRole("button", { name: "Create token" }));
    expect(screen.getByRole("button", { name: "Done" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Create token" })).toBeNull();
  });
});
