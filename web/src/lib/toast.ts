// The toast primitive (#1155) lives in components/ui/Toast (#2485), over
// Kobalte Toast; this module keeps the name every caller already imports.
// pushToast is toast(): raise a notice; the shell's Toaster draws it.
export {
  toast as pushToast,
  toast,
  dismissToast,
  clearToasts,
  toasts,
  DEFAULT_TOAST_TTL_MS,
  MAX_TOASTS,
  type ToastTone,
  type ToastAction,
  type ToastEntry as Toast,
  type ToastInput,
} from "../components/ui/Toast";
