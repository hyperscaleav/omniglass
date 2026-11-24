// A form that names what is missing (#2432). A submit with fields unfilled does
// not grey its button and say nothing: it lists each field that fails, in the
// order the form shows them, and puts the focus on the first. All in TypeScript
// (the repo's validation rules; never native `required`/`min`/`max`), over any
// form's own `errors` record, so FormActionBar (#2440) can lift it as it is.

export type FormProblem = { key: string; label: string; message: string };

// formProblems: the failing fields of an errors record, in `order`.
export function formProblems<K extends string>(errors: Partial<Record<K, string>>, order: readonly K[], labels: Record<K, string>): FormProblem[] {
  return order.filter((k) => !!errors[k]).map((k) => ({ key: k, label: labels[k], message: errors[k]! }));
}

// separator is what sits between the nth of `count` listed items and the next:
// ", " between, " and " before the last, nothing after it.
export function separator(index: number, count: number): string {
  if (index >= count - 1) return "";
  return index === count - 2 ? " and " : ", ";
}

// The control to focus for a registered element: the element itself when it can
// take focus, else the part a composite marks data-field-target (a Picker's
// input), else the first form control inside it.
const FOCUSABLE = "input, select, textarea, button, [tabindex]";
export function focusTarget(el: HTMLElement): HTMLElement | null {
  if (el.matches(FOCUSABLE) && !el.matches("button")) return el;
  return el.querySelector<HTMLElement>("[data-field-target]") ?? el.querySelector<HTMLElement>("input, select, textarea, .cm-content, [contenteditable=true]");
}

// createFieldFocus: a form registers each field's element by key (a `ref`), and
// focusFirst focuses the first of the given keys that is registered, returning
// its key.
export function createFieldFocus() {
  const els = new Map<string, HTMLElement>();
  const finders = new Map<string, () => HTMLElement | null | undefined>();
  return {
    register: (key: string) => (el: HTMLElement) => void els.set(key, el),
    // For a control that cannot take a ref (a composite that renders a fragment):
    // find its focusable element when the time comes.
    registerFinder: (key: string, find: () => HTMLElement | null | undefined) => void finders.set(key, find),
    focus(key: string): boolean {
      const found = finders.get(key)?.();
      const t = found ?? (els.get(key) && focusTarget(els.get(key)!));
      if (!t) return false;
      t.focus();
      t.scrollIntoView?.({ block: "center" });
      return true;
    },
    focusFirst(keys: readonly string[]): string | undefined {
      for (const k of keys) if (this.focus(k)) return k;
      return undefined;
    },
  };
}
