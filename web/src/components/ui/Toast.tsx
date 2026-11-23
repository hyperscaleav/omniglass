import { Show, createSignal, onCleanup, type JSX } from "solid-js";
import { Portal } from "solid-js/web";
import { CloseButton, Description, List, Region, Root, Title, toaster } from "@kobalte/core/toast";
import Button from "../Button";
import { X } from "../icons";

// Toast: a short notice that an action the operator took has finished, or
// failed, raised wherever it ran and drawn bottom right by the shell's one
// Toaster (#1155, #2485), over Kobalte Toast. A toast never carries the only
// copy of anything: it points at where the outcome lives (its action opens it)
// and goes away by itself.
//
// Raise one with toast(): a tone, a title, an optional live body (a render
// function, so it can follow something that moves, a command's phase) and one
// action. toast.success / toast.error / toast.info / toast.warning are the
// one-line forms for an action's result:
//
//   toast.success("Setting saved");
//   toast.error("Could not save the setting", { body: () => message });
//
// Kobalte owns the lifetime and the keys: each toast is a polite live status
// the reader announces without moving the focus; its timer pauses while the
// pointer is over it or the focus is in it; Alt+T moves the focus to the
// notifications region, Tab walks its buttons, Esc closes the focused toast;
// a swipe right dismisses one. MAX_TOASTS show at once and a burst queues
// behind them, so nothing raised is lost. ttlMs 0 keeps a toast until it is
// dismissed. toasts() lists what has been raised and not yet dismissed (the
// shown and the queued), for a test or a surface that reads it.

export type ToastTone = "info" | "success" | "warning" | "error";
export type ToastAction = { label: string; onClick: () => void };
export type ToastEntry = { id: number; tone: ToastTone; title: string; body?: () => JSX.Element; action?: ToastAction };
export type ToastInput = Omit<ToastEntry, "id"> & {
  // How long it stays, in milliseconds; 0 keeps it until dismissed.
  ttlMs?: number;
};

export const DEFAULT_TOAST_TTL_MS = 8_000;
export const MAX_TOASTS = 4;

const [toasts, setToasts] = createSignal<readonly ToastEntry[]>([]);
export { toasts };

const forget = (id: number) => setToasts((prev) => prev.filter((t) => t.id !== id));

// Whole class names, so Tailwind emits them: each toast is a raised card
// edged in its tone's hue (a soft fill alone sinks into the dark page).
const TONE_CLASS: Record<ToastTone, string> = {
  info: "border-info/50",
  success: "border-success/50",
  warning: "border-warning/50",
  error: "border-error/50",
};

function Card(props: { toastId: number; entry: ToastEntry; ttlMs: number }) {
  // Kobalte unmounts a toast when it closes (its time, its close, Esc, a
  // swipe): that is when it leaves toasts().
  onCleanup(() => forget(props.toastId));
  const t = props.entry;
  return (
    <Root
      toastId={props.toastId}
      duration={props.ttlMs > 0 ? props.ttlMs : undefined}
      persistent={props.ttlMs === 0}
      priority={t.tone === "error" ? "high" : "low"}
      data-testid="toast"
      data-tone={t.tone}
      class={`alert flex w-80 max-w-[calc(100vw-2rem)] items-start gap-3 border bg-base-200 py-2.5 text-sm text-base-content shadow-xl focus:outline-none focus-visible:ring-2 focus-visible:ring-primary/60 ${TONE_CLASS[t.tone]}`}
    >
      <div class="flex min-w-0 flex-1 flex-col gap-1">
        <Title class="font-medium">{t.title}</Title>
        <Show when={t.body}>{(body) => <Description class="min-w-0">{body()()}</Description>}</Show>
        <Show when={t.action}>
          {(a) => (
            <div>
              <Button
                size="xs"
                intent="quiet"
                onClick={() => {
                  a().onClick();
                  dismissToast(props.toastId);
                }}
              >
                {a().label}
              </Button>
            </div>
          )}
        </Show>
      </div>
      <CloseButton aria-label="Dismiss" class="btn btn-quiet btn-xs btn-square">
        <X size={14} />
      </CloseButton>
    </Root>
  );
}

export function toast(input: ToastInput): number {
  const { ttlMs = DEFAULT_TOAST_TTL_MS, ...rest } = input;
  // Kobalte may draw the card inside show(), before the id is back.
  const entry: ToastEntry = { ...rest, id: 0 };
  const id = toaster.show((p) => <Card toastId={p.toastId} entry={entry} ttlMs={ttlMs} />);
  entry.id = id;
  setToasts((prev) => [...prev, entry]);
  return id;
}

type Short = Omit<ToastInput, "tone" | "title">;
toast.success = (title: string, rest: Short = {}) => toast({ ...rest, tone: "success", title });
toast.error = (title: string, rest: Short = {}) => toast({ ...rest, tone: "error", title });
toast.info = (title: string, rest: Short = {}) => toast({ ...rest, tone: "info", title });
toast.warning = (title: string, rest: Short = {}) => toast({ ...rest, tone: "warning", title });

export function dismissToast(id: number): void {
  toaster.dismiss(id);
  forget(id);
}

// clearToasts empties the region and its queue (a test's reset).
export function clearToasts(): void {
  toaster.clear();
  setToasts([]);
}

// Toaster: the one region every toast is drawn in, mounted once by the shell.
// Portalled to the body so no card or drawer clips it.
export function Toaster(): JSX.Element {
  return (
    <Portal>
      <Region limit={MAX_TOASTS} duration={DEFAULT_TOAST_TTL_MS} swipeDirection="right">
        <List class="toast toast-end toast-bottom z-70" data-testid="toaster" />
      </Region>
    </Portal>
  );
}
