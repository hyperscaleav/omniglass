import { For, Show, type JSX } from "solid-js";
import { diagnosticHref } from "../lib/expressionEditor";

// A stored warning on one of a rule's expressions (criteria_warnings,
// calc_warnings): where it sits, what it says, and the registered
// diagnostic that gave it.
export type ExpressionNote = { field: string; line?: number; column?: number; code?: string; message: string };

// ExpressionNotes shows a stored rule's warnings (#1021) on its blade: notes,
// never an alert, since the rule judges or computes all the same and a
// warning never refuses a save. Each says where it sits (`where` words the
// field and position) and links its code to the docs' entry, which explains
// the mistake with examples. Nothing renders when there are none.
export default function ExpressionNotes(p: { notes?: ExpressionNote[] | null; where: (n: ExpressionNote) => string; testid: string }): JSX.Element {
  return (
    <Show when={(p.notes ?? []).length > 0}>
      <div role="note" data-testid={p.testid} class="alert alert-warning alert-soft flex flex-col items-start gap-1 text-sm">
        <For each={p.notes}>
          {(n) => (
            <span>
              <span class="font-semibold">Warning</span> ({p.where(n)}): {n.message}.{" "}
              <Show when={n.code}>
                <a class="link font-data text-[12px]" href={diagnosticHref(n.code!)} target="_blank" rel="noreferrer">
                  {n.code}
                </a>
              </Show>
            </span>
          )}
        </For>
        <span class="text-[12px] text-base-content/70">A warning never stops the rule; edit the expression to clear it.</span>
      </div>
    </Show>
  );
}
