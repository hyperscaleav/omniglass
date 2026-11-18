import { createSignal } from "solid-js";

// Handing omnibot context (#2122, #2162): each prompt carries the page the
// person is on as an AG-UI context entry, and a triage the alarm it is on. A
// subject is a reference, a kind and a name or dotted address, or the page as
// its route and arguments: never what the console rendered, so device-written
// text and anything revealed on screen stay out of the model's context, and
// never a uuid. Omnibot reads the data itself, through its tools, under the
// person's own grants. Context is data, not authority.

export type Subject = { kind: string; ref: string };
export type ContextEntry = { description: string; value: string };

// The run's own limits (internal/api/assistant_run.go): at most this many
// entries, each value at most this long.
const MAX_ENTRIES = 20;
const MAX_VALUE = 2000;

const UUID = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i;

// pageSubject is the page as a query: its route and the arguments that are not
// a uuid. A detail page's route holds its entity's uuid, so it is named by the
// entity's name or address instead (#759's name-shaped address); with no name
// a route that holds a uuid is no subject.
export function pageSubject(
  pathname: string,
  search: string,
  name?: string,
): Subject | undefined {
  if (UUID.test(pathname)) {
    if (!name || UUID.test(name)) return undefined;
    pathname = pathname.replace(UUID, name);
  }
  const params = new URLSearchParams(search);
  for (const [k, v] of [...params]) if (UUID.test(v)) params.delete(k, v);
  const q = params.toString();
  return { kind: "page", ref: q ? `${pathname}?${q}` : pathname };
}

// alarmSubject is an alarm by its component's name and the rule that raised
// it, or when it was raised: never its message, which a device may write.
export function alarmSubject(a: {
  component: string;
  raised_by_rule?: string | null;
  raised_at: string;
}): Subject {
  return {
    kind: "alarm",
    ref: a.raised_by_rule
      ? `${a.component}/${a.raised_by_rule}`
      : `${a.component}@${a.raised_at}`,
  };
}

const same = (a: Subject, b: Subject) => a.kind === b.kind && a.ref === b.ref;

// contextEntries is what a run is sent for the subjects: one entry each, in
// order, a duplicate, a uuid or an oversized value left out, at most what a
// run takes.
export function contextEntries(subjects: Subject[]): ContextEntry[] {
  const kept: Subject[] = [];
  for (const s of subjects) {
    if (kept.length === MAX_ENTRIES) break;
    if (
      !s.kind ||
      !s.ref ||
      s.ref.length > MAX_VALUE ||
      UUID.test(s.ref) ||
      kept.some((k) => same(k, s))
    )
      continue;
    kept.push(s);
  }
  return kept.map((s) => ({ description: s.kind, value: s.ref }));
}

const [pageName, setPageName] = createSignal<string | undefined>();

// namePage is the name or address of the entity the page shows, set by the
// Page primitive for a detail page, so omnibot is handed the page by its
// entity's name or address.
export function namePage(name: string | undefined) {
  setPageName(name);
}

// pageHere is the page the person is on, the context every prompt carries.
export function pageHere(pathname: string, search: string): Subject | undefined {
  return pageSubject(pathname, search, pageName());
}

export type SkillCall = { name: string; arguments: Record<string, string> };

// triageCall is the triage-alarm skill run on an alarm subject: its component,
// and the rule that raised it when the alarm is known by one. Nothing for a
// subject that is not an alarm.
export function triageCall(s: Subject): SkillCall | undefined {
  if (s.kind !== "alarm") return undefined;
  const slash = s.ref.indexOf("/");
  const [component, alarm] =
    slash >= 0
      ? [s.ref.slice(0, slash), s.ref.slice(slash + 1)]
      : [s.ref.split("@")[0], ""];
  if (!component) return undefined;
  return {
    name: "triage-alarm",
    arguments: alarm ? { component, alarm } : { component },
  };
}

const [triage, setTriage] = createSignal<Subject | undefined>();

// pendingTriage is an alarm the person asked to triage from outside the panel,
// for the panel to run once it is open.
export const pendingTriage = triage;

export function requestTriage(s: Subject | undefined) {
  setTriage(s);
}
