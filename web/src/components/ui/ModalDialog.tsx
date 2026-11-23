import { type JSX, Show, createComputed, on } from "solid-js";
import { CloseButton, Content, Overlay, Portal, Root, Title } from "@kobalte/core/dialog";
import { Content as AlertContent } from "@kobalte/core/alert-dialog";

// ModalDialog: the console's one modal surface (#2485), over Kobalte Dialog.
// Kobalte owns the focus trap, focus restore to whatever opened it, Esc, the
// overlay click, scroll lock and the ARIA wiring; this owns the portal (so no
// card or drawer clips it) and the house overlay. The panel's shape is the
// caller's: a right slide-over (Drawer), a centred card (ConfirmDialog, the
// enrollment token), a palette near the top (CommandPalette).
//
// alert makes it an alertdialog, for a decision that interrupts (a confirm).
// frameClass wraps the panel in a full-screen positioning layer (a centred
// card); without it the panel positions itself (a slide-over). initialFocus
// picks what takes the focus on open (a confirm hands it to the safe choice);
// by default the first focusable element in the panel does. A title is
// required by the dialog role: render a DialogHeading inside (class="sr-only"
// when the surface reads without one).
//
// The parts are the module's NAMED exports, never the `Dialog.Content` /
// `AlertDialog.Content` namespaces: Kobalte 0.13 builds both namespaces by
// Object.assign onto the one shared root function, so whichever module loads
// last decides what `Dialog.Content` is, and a drawer read as an alertdialog
// once the alert-dialog module was imported anywhere in the bundle.
export interface ModalDialogProps {
  open: boolean;
  onClose: () => void;
  alert?: boolean;
  class?: string;
  classList?: Record<string, boolean | undefined>;
  style?: JSX.CSSProperties;
  overlayClass?: string;
  frameClass?: string;
  initialFocus?: (panel: HTMLElement) => HTMLElement | null | undefined;
  "data-testid"?: string;
  children: JSX.Element;
}

export const OVERLAY_CLASS = "fixed inset-0 z-60 bg-black/45";

export default function ModalDialog(props: ModalDialogProps) {
  let panel: HTMLDivElement | undefined;
  // A setter rather than ref={panel}: Solid compiles both to the same
  // assignment, but a reader (or a linter) sees this one happen.
  const setPanel = (el: HTMLDivElement) => (panel = el);
  // What had the focus when it opened, caught before Kobalte moves it. A
  // dialog opened by state (no Kobalte trigger) otherwise sent the focus to
  // the body on close: Esc out of the New outpost drawer left the keyboard
  // user at the top of the page.
  let opener: HTMLElement | null = null;
  createComputed(
    on(
      () => props.open,
      (o) => {
        if (o && typeof document !== "undefined") opener = document.activeElement as HTMLElement | null;
      },
    ),
  );
  const onCloseAutoFocus = (e: Event) => {
    if (opener && opener !== document.body && opener.isConnected) {
      e.preventDefault();
      opener.focus();
    }
    opener = null;
  };
  const onOpenAutoFocus = (e: Event) => {
    const target = props.initialFocus && panel ? props.initialFocus(panel) : null;
    if (target) {
      e.preventDefault();
      target.focus();
    }
  };
  const panelProps = () => ({
    class: props.class,
    classList: props.classList,
    style: props.style,
    "data-testid": props["data-testid"],
  });
  const content = () => (
    <Show
      when={props.alert}
      fallback={
        <Content ref={setPanel} {...panelProps()} onOpenAutoFocus={onOpenAutoFocus} onCloseAutoFocus={onCloseAutoFocus}>
          {props.children}
        </Content>
      }
    >
      <AlertContent ref={setPanel} {...panelProps()} onOpenAutoFocus={onOpenAutoFocus} onCloseAutoFocus={onCloseAutoFocus}>
        {props.children}
      </AlertContent>
    </Show>
  );
  return (
    <Root open={props.open} onOpenChange={(o) => !o && props.onClose()}>
      <Portal>
        <Overlay class={props.overlayClass ?? OVERLAY_CLASS} style={props.style} />
        <Show when={props.frameClass} fallback={content()}>
          <div class={props.frameClass}>{content()}</div>
        </Show>
      </Portal>
    </Root>
  );
}

// The dialog's heading and accessible name (an h2). Inside a ModalDialog only.
export function DialogHeading(props: { class?: string; children: JSX.Element }) {
  return <Title class={props.class}>{props.children}</Title>;
}

// A close control: closes through the dialog's onClose, as Esc does.
export function DialogClose(props: { class?: string; "aria-label"?: string; children: JSX.Element }) {
  return (
    <CloseButton class={props.class} aria-label={props["aria-label"]}>
      {props.children}
    </CloseButton>
  );
}
