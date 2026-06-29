import { For, Show, onCleanup, createEffect } from "solid-js";
import { useTweaks, setTweak, type Tweaks } from "../lib/tweaks";
import { X } from "./icons";

// The Tweaks slide-over: theme, type system, and density, each a daisyUI
// segmented control (`join` of buttons). A plain signal-driven panel for now; it
// is the first candidate to move to a Kobalte Dialog when the interactive
// surface grows (focus trap, escape, aria), tracked for a later slice.
export default function TweaksPanel(props: { open: boolean; onClose: () => void }) {
  const t = useTweaks();

  // Close on Escape while open.
  createEffect(() => {
    if (!props.open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && props.onClose();
    window.addEventListener("keydown", onKey);
    onCleanup(() => window.removeEventListener("keydown", onKey));
  });

  return (
    <Show when={props.open}>
      <div class="fixed inset-0 z-60 bg-black/45" onClick={props.onClose} />
      <aside role="dialog" aria-label="Display settings" class="fixed inset-y-0 right-0 z-60 flex w-80 flex-col border-l border-base-300 bg-base-100 shadow-2xl">
        <header class="flex items-center justify-between gap-3 border-b border-base-300 px-4 py-3.5">
          <span class="text-sm font-semibold">Display</span>
          <button class="btn btn-ghost btn-sm btn-square" onClick={props.onClose} aria-label="Close"><X size={16} /></button>
        </header>
        <div class="flex flex-col gap-5 p-5">
          <Segmented label="Theme" value={t().theme} options={["dark", "light"]} onChange={(v) => setTweak("theme", v as Tweaks["theme"])} />
          <div>
            <Segmented label="Type system" value={t().type} options={["mixed", "mono"]} onChange={(v) => setTweak("type", v as Tweaks["type"])} />
            <p class="mt-2 px-0.5 text-[11px] leading-relaxed text-base-content/50">
              Mixed pairs IBM Plex Sans for prose with JetBrains Mono for data. Mono is the all-mono face.
            </p>
          </div>
          <Segmented label="Density" value={t().density} options={["comfortable", "compact"]} onChange={(v) => setTweak("density", v as Tweaks["density"])} />
        </div>
      </aside>
    </Show>
  );
}

function Segmented(props: { label: string; value: string; options: string[]; onChange: (v: string) => void }) {
  return (
    <div>
      <div class="eyebrow mb-2">{props.label}</div>
      <div class="join">
        <For each={props.options}>
          {(opt) => (
            <button
              class="btn join-item btn-sm capitalize"
              classList={{ "btn-primary": props.value === opt, "btn-ghost": props.value !== opt }}
              onClick={() => props.onChange(opt)}
            >
              {opt}
            </button>
          )}
        </For>
      </div>
    </div>
  );
}
