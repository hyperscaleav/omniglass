import { type JSX, Show } from "solid-js";
import { Dialog } from "@kobalte/core/dialog";
import { X } from "./icons";

// Drawer: a right slide-over on Kobalte Dialog. Kobalte owns focus-trap, focus
// restore, Esc, scroll-lock, and the ARIA wiring; this only styles the shell.
// headerExtra is a slot beside the close button (e.g. a maximize button for a
// detail blade).
export default function Drawer(props: {
  open: boolean;
  onClose: () => void;
  title: JSX.Element;
  headerExtra?: JSX.Element;
  children: JSX.Element;
}) {
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
          <div class="flex-1 overflow-auto p-5">{props.children}</div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog>
  );
}
