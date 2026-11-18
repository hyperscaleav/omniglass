import { createSignal } from "solid-js";

// Handing omnibot context (#2122): the person gathers subjects from the
// console into the panel's tray, and the next prompt carries them as AG-UI
// context entries. A subject is a reference, a kind and a name or dotted
// address, or the page as its route and arguments: never what the console
// rendered, so device-written text and anything revealed on screen stay out
// of the model's context, and never a uuid. Omnibot reads the data itself,
// through its tools, under the person's own grants. Context is data, not
// authority.

export type Subject = { kind: string; ref: string };
export type ContextEntry = { description: string; value: string };

// The run's own limits (internal/api/assistant_run.go): at most this many
// entries, each value at most this long.
const MAX_ENTRIES = 20;
const MAX_VALUE = 2000;

const UUID = /[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/i;

// pageSubject is the page as a query: its route and the arguments that are not
// a uuid. A route that holds a uuid is no subject.
export function pageSubject(pathname: string, search: string): Subject | undefined {
  if (UUID.test(pathname)) return undefined;
  const params = new URLSearchParams(search);
  for (const [k, v] of [...params]) if (UUID.test(v)) params.delete(k, v);
  const q = params.toString();
  return { kind: "page", ref: q ? `${pathname}?${q}` : pathname };
}

// subjectOf is the subject an element is stamped with (data-subject-kind and
// data-subject-ref), read from the stamp alone.
export function subjectOf(el: HTMLElement): Subject | undefined {
  const kind = el.dataset.subjectKind;
  const ref = el.dataset.subjectRef;
  if (!kind || !ref || UUID.test(ref)) return undefined;
  return { kind, ref };
}

const same = (a: Subject, b: Subject) => a.kind === b.kind && a.ref === b.ref;

// contextEntries is what a run is sent for the subjects: one entry each, in
// order, a duplicate, a uuid or an oversized value left out, at most what a
// run takes.
export function contextEntries(subjects: Subject[]): ContextEntry[] {
  const kept: Subject[] = [];
  for (const s of subjects) {
    if (kept.length === MAX_ENTRIES) break;
    if (!s.kind || !s.ref || s.ref.length > MAX_VALUE || UUID.test(s.ref) || kept.some((k) => same(k, s))) continue;
    kept.push(s);
  }
  return kept.map((s) => ({ description: s.kind, value: s.ref }));
}

const [subjects, setSubjects] = createSignal<Subject[]>([]);

// tray is what the person has gathered for the next prompt.
export const tray = subjects;

export function addSubjects(add: Subject[]) {
  setSubjects((cur) => [...cur, ...add.filter((s, i) => !cur.some((c) => same(c, s)) && add.findIndex((t) => same(t, s)) === i)]);
}

export function removeSubject(s: Subject) {
  setSubjects((cur) => cur.filter((c) => !same(c, s)));
}

export function clearTray() {
  setSubjects([]);
}

// isHandOverKey is the key that hands omnibot the console: ⌘J or Ctrl-J, beside
// the palette's ⌘K.
export function isHandOverKey(e: Pick<KeyboardEvent, "key" | "metaKey" | "ctrlKey" | "altKey" | "shiftKey">): boolean {
  return (e.metaKey || e.ctrlKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "j";
}

// handOverPage puts the page in the tray when the person has gathered nothing
// else: what they are looking at is the context.
export function handOverPage(pathname: string, search: string) {
  if (subjects().length > 0) return;
  const page = pageSubject(pathname, search);
  if (page) addSubjects([page]);
}
