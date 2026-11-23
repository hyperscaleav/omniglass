import { type Component, type JSX, Show } from "solid-js";
import Button from "../Button";
import PanelFooter from "../PanelFooter";
import { X } from "../icons";
import ModalDialog, { DialogClose, DialogHeading } from "./ModalDialog";

// ConfirmDialog is the console's confirm for a decision that needs reading
// before it is made (#1078, trusting a changed key): a centred ModalDialog
// in its alert role (role alertdialog, focus trapped and restored, Esc to close,
// portalled so no card clips it), its body the host's and its rail the one
// PanelFooter draws. A delete keeps the browser's own confirm(); this is for a
// confirm whose body has to teach, which a native prompt cannot hold.
//
// The safe choice holds the focus when it opens: Enter on a freshly opened
// confirm cancels, never confirms. Cancel, Esc and the overlay all close it
// through onClose and confirm nothing; only the confirm button calls
// onConfirm, and it holds while busy or disabled. error is the host's account
// of a confirm the server refused, shown as an alert above the rail.
export default function ConfirmDialog(props: {
  open: boolean;
  onClose: () => void;
  title: JSX.Element;
  children: JSX.Element;
  confirmLabel: string;
  confirmIcon?: Component<{ size?: number }>;
  onConfirm: () => void;
  busy?: boolean;
  disabled?: boolean;
  error?: string | null;
  cancelLabel?: string;
}) {
  return (
    <ModalDialog
      alert
      open={props.open}
      onClose={props.onClose}
      overlayClass="fixed inset-0 z-[70] bg-black/50"
      frameClass="fixed inset-0 z-[70] flex items-center justify-center p-4"
      class="flex max-h-[90vh] w-full max-w-lg flex-col overflow-hidden rounded-box border border-base-300 bg-base-100 shadow-2xl"
      // The rail's first button is Cancel: the safe choice takes the focus.
      initialFocus={(panel) => panel.querySelector<HTMLButtonElement>("footer button")}
    >
      <header class="flex items-center justify-between gap-3 border-b border-base-300 px-5 py-3">
        <DialogHeading class="min-w-0 flex-1 text-sm font-semibold">{props.title}</DialogHeading>
        <DialogClose class="btn btn-quiet btn-sm btn-square" aria-label="Close">
          <X size={16} />
        </DialogClose>
      </header>
      <div class="flex-1 overflow-auto px-5 py-4">{props.children}</div>
      <Show when={props.error}>
        <div role="alert" class="mx-5 mb-3 rounded-box border border-error/40 bg-error/5 px-3 py-2 text-xs text-error">
          {props.error}
        </div>
      </Show>
      <PanelFooter>
        <div class="ml-auto flex items-center gap-2">
          <Button icon={X} onClick={() => props.onClose()} disabled={props.busy}>
            {props.cancelLabel ?? "Cancel"}
          </Button>
          <Button
            intent="action"
            icon={props.confirmIcon}
            loading={props.busy}
            disabled={props.disabled}
            onClick={() => props.onConfirm()}
          >
            {props.confirmLabel}
          </Button>
        </div>
      </PanelFooter>
    </ModalDialog>
  );
}
