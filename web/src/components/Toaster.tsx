import { For, Show, type JSX } from "solid-js";
import { Portal } from "solid-js/web";
import Button from "./Button";
import { X } from "./icons";
import { dismissToast, toasts, type ToastTone } from "../lib/toast";

// Toaster draws the toast store (lib/toast), bottom right above the page: a
// polite live region, so a screen reader announces each notice without moving
// focus, each toast a raised card edged in its tone's hue (a soft fill alone
// sinks into the dark page), with its action and a close. Portalled to the body
// so no card or drawer clips it. Whole class names, so Tailwind emits them.
const TONE_CLASS: Record<ToastTone, string> = {
  info: "border-info/50",
  success: "border-success/50",
  warning: "border-warning/50",
  error: "border-error/50",
};

export default function Toaster(): JSX.Element {
  return (
    <Portal>
      <div class="toast toast-end toast-bottom z-70" role="status" aria-live="polite" data-testid="toaster">
        <For each={toasts()}>
          {(t) => (
            <div class={`alert flex w-88 max-w-[calc(100vw-2rem)] items-start gap-3 border bg-base-200 py-2.5 text-sm text-base-content shadow-xl ${TONE_CLASS[t.tone]}`} data-testid="toast">
              <div class="flex min-w-0 flex-1 flex-col gap-1">
                <span class="font-medium">{t.title}</span>
                <Show when={t.body}>{(body) => <div class="min-w-0">{body()()}</div>}</Show>
                <Show when={t.action}>
                  {(a) => (
                    <div>
                      <Button size="xs" intent="quiet" onClick={() => { a().onClick(); dismissToast(t.id); }}>
                        {a().label}
                      </Button>
                    </div>
                  )}
                </Show>
              </div>
              <Button square size="xs" intent="quiet" icon={X} label="Dismiss" onClick={() => dismissToast(t.id)} />
            </div>
          )}
        </For>
      </div>
    </Portal>
  );
}
