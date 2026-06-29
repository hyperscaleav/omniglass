import { Show } from "solid-js";
import { useTweaks, setTweak } from "../lib/tweaks";
import { Sun, Moon, Search, Sliders } from "./icons";

// The sticky top bar (daisyUI navbar): the current section label, a reserved
// search affordance, the theme toggle, and the Tweaks trigger.
export default function TopBar(props: { section: string; onOpenTweaks: () => void }) {
  const t = useTweaks();
  return (
    <header class="navbar sticky top-0 z-20 min-h-14 gap-3 border-b border-base-300 bg-base-100/80 px-6 backdrop-blur">
      <span class="eyebrow text-base-content/70">{props.section}</span>
      <div class="flex-1" />
      <label class="input input-sm hidden w-56 items-center gap-2 border-base-300 bg-base-200 text-base-content/40 sm:flex" title="Search (reserved)">
        <Search size={15} />
        <span>Search</span>
        <kbd class="kbd kbd-sm ml-auto">⌘K</kbd>
      </label>
      <button class="btn btn-ghost btn-sm btn-square" title={t().theme === "dark" ? "Light mode" : "Dark mode"} onClick={() => setTweak("theme", t().theme === "dark" ? "light" : "dark")}>
        <Show when={t().theme === "dark"} fallback={<Moon size={16} />}><Sun size={16} /></Show>
      </button>
      <button class="btn btn-ghost btn-sm btn-square" title="Display settings" onClick={props.onOpenTweaks}>
        <Sliders size={16} />
      </button>
    </header>
  );
}
