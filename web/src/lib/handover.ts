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
export function pageSubject(
  pathname: string,
  search: string,
): Subject | undefined {
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

const STAMP = "[data-subject-kind]";
const CONTROL = "button,a,input,select,textarea,label,summary";

// gathered is what an alt-click on target gathers: the stamped element it
// lands in, or with range every stamped sibling from the anchor (the last one
// gathered) to it. A click on a control inside a stamp gathers nothing, so the
// control keeps its own meaning (an outline chevron's alt expands all).
export function gathered(
  target: Element,
  anchor: Element | undefined,
  range: boolean,
): { stamp: HTMLElement; subjects: Subject[] } | undefined {
  const stamp = target.closest<HTMLElement>(STAMP);
  if (!stamp) return undefined;
  const control = target.closest(CONTROL);
  if (control && control !== stamp && stamp.contains(control)) return undefined;
  const own = subjectOf(stamp);
  if (!own) return undefined;
  const list = stamp.parentElement;
  if (
    !range ||
    !anchor ||
    anchor === stamp ||
    !list ||
    anchor.parentElement !== list
  )
    return { stamp, subjects: [own] };
  const kids = [...list.children];
  const [from, to] = [kids.indexOf(anchor), kids.indexOf(stamp)].sort(
    (x, y) => x - y,
  );
  const subjects = kids
    .slice(from, to + 1)
    .map((el) =>
      el instanceof HTMLElement && el.matches(STAMP)
        ? subjectOf(el)
        : undefined,
    )
    .filter((s): s is Subject => !!s);
  return { stamp, subjects };
}

// stamped is the subject a primitive stamps on its row, as data-subject-kind
// and data-subject-ref; nothing for an empty reference or a uuid.
export function stamped(s: Subject | undefined): Subject | undefined {
  return s && s.kind && s.ref && !UUID.test(s.ref) ? s : undefined;
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

const [subjects, setSubjects] = createSignal<Subject[]>([]);

// tray is what the person has gathered for the next prompt.
export const tray = subjects;

export function addSubjects(add: Subject[]) {
  setSubjects((cur) => [
    ...cur,
    ...add.filter(
      (s, i) =>
        !cur.some((c) => same(c, s)) && add.findIndex((t) => same(t, s)) === i,
    ),
  ]);
}

export function removeSubject(s: Subject) {
  setSubjects((cur) => cur.filter((c) => !same(c, s)));
}

export function clearTray() {
  setSubjects([]);
}

// gatherer is the console's click listener, for the capture phase: an
// alt-click on a stamped row adds its subject to the tray, a shift-alt-click
// every row from the last one gathered, and opens omnibot. Not ⌘ or Ctrl,
// which open a link in a new tab. Capturing runs ahead of Solid's delegated
// handlers, so the row's own click does not run as well.
export function gatherer(
  mayAsk: () => boolean,
  open: () => void,
): (e: MouseEvent) => void {
  let anchor: Element | undefined;
  return (e) => {
    if (
      !e.altKey ||
      e.metaKey ||
      e.ctrlKey ||
      !mayAsk() ||
      !(e.target instanceof Element)
    )
      return;
    const g = gathered(e.target, anchor, e.shiftKey);
    if (!g) return;
    e.preventDefault();
    e.stopPropagation();
    addSubjects(g.subjects);
    anchor = g.stamp;
    open();
  };
}

// isHandOverKey is the key that hands omnibot the console: ⌘J or Ctrl-J, beside
// the palette's ⌘K.
export function isHandOverKey(
  e: Pick<KeyboardEvent, "key" | "metaKey" | "ctrlKey" | "altKey" | "shiftKey">,
): boolean {
  return (
    (e.metaKey || e.ctrlKey) &&
    !e.altKey &&
    !e.shiftKey &&
    e.key.toLowerCase() === "j"
  );
}

// handOverPage puts the page in the tray when the person has gathered nothing
// else: what they are looking at is the context.
export function handOverPage(pathname: string, search: string) {
  if (subjects().length > 0) return;
  const page = pageSubject(pathname, search);
  if (page) addSubjects([page]);
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

// pendingTriage is an alarm the person asked to triage from outside the panel
// (the palette), for the panel to run once it is open.
export const pendingTriage = triage;

export function requestTriage(s: Subject | undefined) {
  setTriage(s);
}
