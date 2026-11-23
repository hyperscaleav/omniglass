import type { RulePreview } from "./expressions";
import { sustainWords } from "./event_rules";

// What a rule preview says, in words (#2432): the pure function from the
// server's answer (POST /event-rules:preview) and the draft's own words to the
// sentences the panel shows. The server judges each sample alone and never
// applies a sustain (it says so: sustains_ignored), so the sentences name that
// plainly when the operator typed one, instead of a footnote contradicting the
// form.

export type PreviewContext = {
  // The metric or property the rule reads, as the operator sees it.
  reads: string;
  // The condition in the operator's words ("Temperature above 40 °C"), as the
  // form reads it back.
  condition: string;
  // The sustains as typed ("30s"); blank or zero says nothing.
  fireSustain?: string;
  clearSustain?: string;
};

export type PreviewWords = {
  sentence: string;
  // Why the typed sustain is not in the counts; null when none was typed.
  sustain: string | null;
  // The series whose state began unknown; null when none.
  unknown: string | null;
  // The row cap's effect; null when it did not bite.
  cap: string | null;
};

type Series = { component: string; instance?: string };

// seriesLabel names a series by its owner and the metric: "Display 1 ·
// Temperature", with the endpoint when it has one.
export function seriesLabel(s: Series, reads: string): string {
  return `${s.component} · ${reads}${s.instance ? ` (${s.instance})` : ""}`;
}

// windowWords: "24h0m0s" is 24h, as the rest of the console says a Go duration.
export function windowWords(w: string | undefined): string {
  return w ? sustainWords(w) || w : "the window";
}

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;
const times = (n: number) => (n === 1 ? "once" : `${n} times`);
const key = (s: Series) => `${s.component}\u0000${s.instance ?? ""}`;
const who = (s: Series) => (s.instance ? `${s.component} ${s.instance}` : s.component);
const names = (list: Series[], max = 3) => {
  const seen = [...new Set(list.map(who))];
  return seen.length > max ? `${seen.slice(0, max).join(", ")} and ${seen.length - max} more` : seen.join(", ");
};

export function previewWords(r: RulePreview, c: PreviewContext): PreviewWords {
  const win = windowWords(r.window);
  const edges = r.edges ?? [];
  const fires = edges.filter((e) => e.kind === "fire");
  const clears = edges.filter((e) => e.kind === "clear");
  const inForce = r.in_force_now ?? [];
  const unknown = r.state_unknown ?? [];
  let sentence: string;
  if (r.series_total === 0) {
    sentence = `It watches no series of ${c.reads}, so there is nothing to judge it against.`;
  } else if (fires.length === 0) {
    sentence =
      inForce.length > 0
        ? `In the last ${win}, ${c.condition} would not have fired, but ${plural(inForce.length, "series", "series")} (${names(inForce)}) was already in force when the window began and stays so.`
        : `In the last ${win}, no sample met ${c.condition} across the ${r.series_total} series it watches, so it would not have fired.`;
    if (inForce.length > 1) sentence = sentence.replace("was already", "were already");
  } else {
    const on = new Set(fires.map(key)).size;
    sentence = `In the last ${win}, ${c.condition} would have fired ${times(fires.length)} on ${on} of ${r.series_judged} series (${names(fires)})`;
    if (clears.length > 0) sentence += ` and cleared ${times(clears.length)}`;
    sentence += ".";
    if (inForce.length > 0) sentence += ` Right now ${plural(inForce.length, "series", "series")} (${names(inForce)}) would be in force.`;
  }

  const fs = sustainWords(c.fireSustain);
  const cs = sustainWords(c.clearSustain);
  let sustain: string | null = null;
  if (fs || cs) {
    const held = [fs && `hold for at least ${fs} before firing`, cs && `hold for at least ${cs} before clearing`].filter(Boolean).join(" and ");
    const worth = fs ? `a reading that passed within ${fs} would still show here as a firing` : `a reading that came back within ${cs} would still show here as a clearing`;
    sustain = `You asked for it to ${held}. The preview cannot apply that: it judges each sample alone, so ${worth}. Read the counts as the most it would have done.`;
  }

  return {
    sentence,
    sustain,
    unknown:
      unknown.length > 0
        ? `${plural(unknown.length, "series", "series")} (${names(unknown)}) had no sample in the week before the window, so ${unknown.length === 1 ? "its first reading set its" : "their first reading set their"} state and counted no firing.`
        : null,
    cap: r.truncated ? `Judged ${r.series_judged} of ${r.series_total} series: the rest would pass the ${r.row_cap.toLocaleString("en-US")}-sample cap.` : null,
  };
}
