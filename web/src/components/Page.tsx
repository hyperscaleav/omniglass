import type { JSX } from "solid-js";
import { Show } from "solid-js";

// Page scaffold from the design's primitives: a title + optional subtitle and
// right-aligned actions, then the page body, stacked with the density gap.
export default function Page(props: {
  title: string;
  subtitle?: string;
  actions?: JSX.Element;
  children: JSX.Element;
}) {
  return (
    <section class="fade-in" style={{ display: "flex", "flex-direction": "column", gap: "var(--gap-stack)" }}>
      <div style={{ display: "flex", "align-items": "flex-start", "justify-content": "space-between", gap: "16px", "flex-wrap": "wrap" }}>
        <div style={{ "min-width": 0 }}>
          <h1 style={{ "font-size": "24px", "font-weight": 600, "letter-spacing": "-0.02em" }}>{props.title}</h1>
          <Show when={props.subtitle}>
            <p style={{ "margin-top": "5px", "font-size": "13.5px", color: "var(--text-dim)", "max-width": "720px" }}>{props.subtitle}</p>
          </Show>
        </div>
        <Show when={props.actions}>
          <div style={{ display: "flex", "align-items": "center", gap: "8px", flex: "none" }}>{props.actions}</div>
        </Show>
      </div>
      {props.children}
    </section>
  );
}
