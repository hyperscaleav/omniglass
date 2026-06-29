import { createSignal, onCleanup } from "solid-js";

// Relative time ("3m ago", "in 2h"), ported from the design's primitives.
export function rel(iso: string): string {
  const diff = (Date.now() - new Date(iso).getTime()) / 1000;
  const fut = diff < 0;
  const a = Math.abs(diff);
  const f =
    a < 60 ? `${Math.round(a)}s` : a < 3600 ? `${Math.round(a / 60)}m` : a < 86400 ? `${Math.round(a / 3600)}h` : `${Math.round(a / 86400)}d`;
  return fut ? `in ${f}` : `${f} ago`;
}

export function fmtTime(iso: string): string {
  const d = new Date(iso);
  return isNaN(d.getTime()) ? iso : d.toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

// useIsMobile tracks the tablet/phone breakpoint, for the sidebar drawer.
export function useIsMobile(bp = 820) {
  const [m, setM] = createSignal(typeof window !== "undefined" ? window.innerWidth < bp : false);
  const on = () => setM(window.innerWidth < bp);
  if (typeof window !== "undefined") {
    window.addEventListener("resize", on);
    onCleanup(() => window.removeEventListener("resize", on));
  }
  return m;
}
