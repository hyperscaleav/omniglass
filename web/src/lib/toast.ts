import { createSignal, type JSX } from "solid-js";

// The toast primitive (#1155): a short notice that an action the operator took
// has started or finished, raised wherever the action ran and drawn in one place
// by the shell's Toaster, bottom right, above the page. A toast never carries
// the only copy of anything: it points at where the outcome lives (its action
// opens it) and goes away by itself, or when dismissed.
//
// The store is module state, one per console, so any surface can raise one
// without a provider; the Toaster is its only reader. A body is a render
// function, so a toast can show something live (a command's phase as it moves)
// rather than a string frozen at the moment it was raised.

export type ToastTone = "info" | "success" | "warning" | "error";

export type ToastAction = { label: string; onClick: () => void };

export type Toast = {
  id: number;
  tone: ToastTone;
  title: string;
  body?: () => JSX.Element;
  action?: ToastAction;
};

export type ToastInput = Omit<Toast, "id"> & {
  // How long it stays, in milliseconds; 0 keeps it until dismissed.
  ttlMs?: number;
};

export const DEFAULT_TOAST_TTL_MS = 8_000;
// The most toasts on screen at once: a burst keeps the newest.
export const MAX_TOASTS = 4;

const [toasts, setToasts] = createSignal<readonly Toast[]>([]);
let nextID = 1;

export { toasts };

export function pushToast(t: ToastInput): number {
  const id = nextID++;
  const { ttlMs = DEFAULT_TOAST_TTL_MS, ...toast } = t;
  setToasts((prev) => [...prev, { ...toast, id }].slice(-MAX_TOASTS));
  if (ttlMs > 0) setTimeout(() => dismissToast(id), ttlMs);
  return id;
}

export function dismissToast(id: number): void {
  setToasts((prev) => prev.filter((t) => t.id !== id));
}

// clearToasts empties the store (a test's reset).
export function clearToasts(): void {
  setToasts([]);
}
