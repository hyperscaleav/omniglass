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

// describeError pulls a human message out of a thrown API error: openapi-fetch
// surfaces the parsed Huma problem body ({ title, detail, status }), so String()
// on it would yield "[object Object]". Falls back to a generic line.
//
// A SCHEMA refusal is the case the head line alone cannot carry. Huma's `detail`
// for one is always the literal "validation failed", and everything an operator
// could act on is in `errors[]`: which field, and what was wrong with it. Those
// are appended, `body.` stripped from the location because an operator is
// looking at a field rather than at a request, and duplicates collapsed because
// one value failing two keywords (a minLength and a pattern, say) reports twice
// and says one thing.
export function describeError(e: unknown): string {
  const o = e as { detail?: string; title?: string; errors?: { message?: string; location?: string }[] } | null | undefined;
  const head = o?.detail ?? o?.title ?? "The operation failed.";
  const seen = new Set<string>();
  for (const err of o?.errors ?? []) {
    const message = (err?.message ?? "").trim();
    if (!message) continue;
    const where = (err?.location ?? "").replace(/^body\./, "").trim();
    seen.add(where ? `${where}: ${message}` : message);
  }
  return seen.size ? `${head}: ${[...seen].join("; ")}` : head;
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
