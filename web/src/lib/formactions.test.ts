import { describe, it, expect, vi } from "vitest";
import { createRoot } from "solid-js";
import { createFormActions } from "./formactions";

describe("createFormActions", () => {
  it("starts unbound", () => {
    createRoot((dispose) => {
      const a = createFormActions();
      expect(a.binding()).toBeUndefined();
      dispose();
    });
  });

  it("bind publishes the binding", () => {
    createRoot((dispose) => {
      const a = createFormActions();
      a.bind({ submitLabel: "Create user", submit: () => {} });
      expect(a.binding()?.submitLabel).toBe("Create user");
      dispose();
    });
  });

  it("disposing the binder's owner clears the binding", () => {
    createRoot((outer) => {
      const a = createFormActions();
      let disposeInner = () => {};
      createRoot((d) => {
        disposeInner = d;
        a.bind({ submitLabel: "Create user", submit: () => {} });
      });
      expect(a.binding()).toBeDefined();
      disposeInner();
      expect(a.binding()).toBeUndefined();
      outer();
    });
  });

  // The swap case: a Drawer whose body is a <Show> with a footer in each branch.
  // Solid may dispose the outgoing branch AFTER the incoming one has bound, so a
  // naive cleanup would clear the binding that just replaced it. Cleanup must only
  // clear its OWN binding.
  it("a stale cleanup does not clobber a newer binding", () => {
    createRoot((outer) => {
      const a = createFormActions();
      let disposeFirst = () => {};
      createRoot((d) => {
        disposeFirst = d;
        a.bind({ submitLabel: "Create token", submit: () => {} });
      });
      a.bind({ submitLabel: "Done", submit: () => {} });
      disposeFirst();
      expect(a.binding()?.submitLabel).toBe("Done");
      outer();
    });
  });

  it("carries the optional cancel, busy, and disabled accessors through", () => {
    createRoot((dispose) => {
      const a = createFormActions();
      const cancel = vi.fn();
      a.bind({
        submitLabel: "Upload file",
        submit: () => {},
        cancel,
        busy: () => true,
        disabled: () => true,
      });
      const b = a.binding()!;
      expect(b.busy?.()).toBe(true);
      expect(b.disabled?.()).toBe(true);
      b.cancel?.();
      expect(cancel).toHaveBeenCalledOnce();
      dispose();
    });
  });
});
