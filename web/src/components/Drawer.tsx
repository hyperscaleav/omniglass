import { type JSX, Show } from "solid-js";
import { Dialog } from "@kobalte/core/dialog";
import { X } from "./icons";
import Button from "./Button";
import PanelFooter from "./PanelFooter";
import { FormActionsContext, createFormActions } from "../lib/formactions";

// Drawer: a right slide-over on Kobalte Dialog. Kobalte owns focus-trap, focus
// restore, Esc, scroll-lock, and the ARIA wiring; this styles the shell and owns
// the action rail.
//
// The rail belongs to the Drawer, not to the body. A form body registers what its
// buttons DO (see lib/formactions) and renders fields only; this draws the pinned
// bar, exactly as BladeStack draws the blade's. A body that binds nothing gets no
// bar, which is right for a read-only slide-over. headerExtra is a slot beside the
// close button (e.g. a maximize button for a detail blade).
export default function Drawer(props: {
  open: boolean;
  onClose: () => void;
  title: JSX.Element;
  headerExtra?: JSX.Element;
  children: JSX.Element;
}) {
  const actions = createFormActions();
  return (
    <Dialog open={props.open} onOpenChange={(o) => !o && props.onClose()}>
      <Dialog.Portal>
        <Dialog.Overlay class="fixed inset-0 z-60 bg-black/45" />
        <Dialog.Content class="fixed inset-y-0 right-0 z-60 flex w-full max-w-md flex-col border-l border-base-300 bg-base-100 shadow-2xl sm:max-w-md">
          <header class="flex items-center justify-between gap-3 border-b border-base-300 px-4 py-3">
            <Dialog.Title class="min-w-0 flex-1 truncate text-sm font-semibold">{props.title}</Dialog.Title>
            <div class="flex flex-none items-center gap-1">
              <Show when={props.headerExtra}>{props.headerExtra}</Show>
              <Dialog.CloseButton class="btn btn-quiet btn-sm btn-square" aria-label="Close">
                <X size={16} />
              </Dialog.CloseButton>
            </div>
          </header>
          <FormActionsContext.Provider value={actions}>
            <div class="flex-1 overflow-auto p-5">{props.children}</div>
            <Show when={actions.binding()}>
              {(b) => (
                <PanelFooter>
                  <div class="ml-auto flex items-center gap-2">
                    <Show when={b().cancel}>
                      {(c) => (
                        <Button icon={X} onClick={() => c()()} disabled={b().busy?.()}>
                          {b().cancelLabel ?? "Cancel"}
                        </Button>
                      )}
                    </Show>
                    <Button
                      intent="action"
                      icon={b().submitIcon}
                      onClick={() => b().submit()}
                      loading={b().busy?.()}
                      disabled={b().disabled?.()}
                    >
                      {b().submitLabel}
                    </Button>
                  </div>
                </PanelFooter>
              )}
            </Show>
          </FormActionsContext.Provider>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog>
  );
}
