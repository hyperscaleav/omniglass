import { For, Show } from "solid-js";
import { useTweaks, setTweak, type Tweaks } from "../lib/tweaks";
import { X } from "./icons";

// The Tweaks slide-over from the design: theme, type system, and density. Each
// is a segmented radio bound to the shared tweak state.
export default function TweaksPanel(props: { open: boolean; onClose: () => void }) {
  const t = useTweaks();
  return (
    <Show when={props.open}>
      <div onClick={props.onClose} style={{ position: "fixed", inset: 0, "z-index": 60, background: "rgba(0,0,0,.45)" }} />
      <aside
        role="dialog"
        style={{
          position: "fixed", top: 0, bottom: 0, right: 0, "z-index": 61, width: "320px", display: "flex", "flex-direction": "column",
          background: "var(--ground)", "border-left": "1px solid var(--line)", "box-shadow": "var(--shadow-pop)",
        }}
      >
        <header style={{ display: "flex", "align-items": "center", "justify-content": "space-between", gap: "12px", "border-bottom": "1px solid var(--line)", padding: "14px 16px" }}>
          <span style={{ "font-size": "14px", "font-weight": 600 }}>Display</span>
          <button class="btn btn-ghost btn-sm btn-icon" onClick={props.onClose} aria-label="Close"><X size={16} /></button>
        </header>
        <div style={{ padding: "18px", display: "flex", "flex-direction": "column", gap: "20px" }}>
          <Segmented label="Theme" value={t().theme} options={["dark", "light"]} onChange={(v) => setTweak("theme", v as Tweaks["theme"])} />
          <div>
            <Segmented label="Type system" value={t().type} options={["mixed", "mono"]} onChange={(v) => setTweak("type", v as Tweaks["type"])} />
            <p style={{ "font-size": "11px", color: "var(--text-dim)", margin: "8px 2px 0", "line-height": 1.45 }}>
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
      <div class="eyebrow" style={{ "margin-bottom": "8px" }}>{props.label}</div>
      <div style={{ display: "inline-flex", gap: "4px", padding: "3px", background: "var(--raised)", border: "1px solid var(--line)", "border-radius": "var(--r-field)" }}>
        <For each={props.options}>
          {(opt) => (
            <button
              onClick={() => props.onChange(opt)}
              style={{
                padding: "5px 14px", "border-radius": "var(--r-selector)", border: "none", cursor: "pointer", "font-size": "12.5px", "font-family": "var(--font-ui)", "text-transform": "capitalize",
                background: props.value === opt ? "var(--primary)" : "transparent",
                color: props.value === opt ? "var(--primary-ink)" : "var(--text-soft)",
                "font-weight": props.value === opt ? 600 : 400,
              }}
            >
              {opt}
            </button>
          )}
        </For>
      </div>
    </div>
  );
}
