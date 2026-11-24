// Relative time ("3m ago", "in 2h"), ported from the design's primitives.
export function rel(iso: string, now: number = Date.now()): string {
  const diff = (now - new Date(iso).getTime()) / 1000;
  const fut = diff < 0;
  const a = Math.abs(diff);
  const f =
    a < 60 ? `${Math.round(a)}s` : a < 3600 ? `${Math.round(a / 60)}m` : a < 86400 ? `${Math.round(a / 3600)}h` : `${Math.round(a / 86400)}d`;
  return fut ? `in ${f}` : `${f} ago`;
}

// describeError pulls a human message out of a thrown API error: the client
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

// The console's date and number formatting lives here and nowhere else:
// src/date-guard.test.ts fails a toLocale* call in any other file, so the audit
// trail, the file list, a drawer and a chart read an instant the same way (#2447).
// A list shows an instant as relative time (rel) with this absolute form on hover
// (components/Time).

// fmtTime is an instant as a short absolute: "Nov 23, 01:56 PM". A value that is
// not a date is returned as it came.
export function fmtTime(iso: string): string {
  const d = new Date(iso);
  return isNaN(d.getTime()) ? iso : d.toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

// fmtClock is the time of day of an instant, "01:56 PM", to the second when asked.
export function fmtClock(iso: string, opts?: { seconds?: boolean }): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  return d.toLocaleTimeString([], opts?.seconds ? { hour: "2-digit", minute: "2-digit", second: "2-digit" } : { hour: "2-digit", minute: "2-digit" });
}

// fmtDay is the calendar day of an instant (UTC unless local is set), "Nov 23", with the year when it
// is asked for ("Nov 23, 2026"; a certificate outlives the calendar year).
export function fmtDay(iso: string, opts?: { year?: boolean; local?: boolean }): string {
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso;
  return d.toLocaleDateString("en-US", { ...(opts?.year ? { year: "numeric" } : {}), month: "short", day: "numeric", ...(opts?.local ? {} : { timeZone: "UTC" }) });
}

// fmtCount is a whole number with its thousands grouped.
export function fmtCount(n: number): string {
  return n.toLocaleString("en-US");
}
