import { Show } from "solid-js";
import { useTweaks, setTweak } from "../lib/tweaks";
import { Sun, Moon, Search, Sliders } from "./icons";

// The sticky top bar: the current section label, a reserved search affordance,
// the theme toggle, and the Tweaks trigger. Matches the design's TopBar.
export default function TopBar(props: { section: string; onOpenTweaks: () => void }) {
  const t = useTweaks();
  return (
    <header
      style={{
        position: "sticky", top: 0, "z-index": 20, height: "56px", display: "flex", "align-items": "center", gap: "14px",
        padding: "0 24px", "border-bottom": "1px solid var(--line)",
        background: "color-mix(in oklch, var(--ground) 82%, transparent)", "backdrop-filter": "blur(8px)",
      }}
    >
      <span class="eyebrow" style={{ color: "var(--text-soft)" }}>{props.section}</span>
      <div style={{ flex: 1 }} />
      <button class="btn btn-sm" style={{ width: "220px", "justify-content": "flex-start", color: "var(--text-faint)", "font-weight": 400 }} title="Search (reserved)">
        <Search size={15} /><span>Search</span>
        <span class="mono" style={{ "margin-left": "auto", "font-size": "11px", color: "var(--text-faint)" }}>⌘K</span>
      </button>
      <button class="btn btn-ghost btn-sm btn-icon" title={t().theme === "dark" ? "Light mode" : "Dark mode"} onClick={() => setTweak("theme", t().theme === "dark" ? "light" : "dark")}>
        <Show when={t().theme === "dark"} fallback={<Moon size={16} />}><Sun size={16} /></Show>
      </button>
      <button class="btn btn-ghost btn-sm btn-icon" title="Display settings" onClick={props.onOpenTweaks}>
        <Sliders size={16} />
      </button>
    </header>
  );
}
