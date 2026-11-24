import { For, Show, type JSX } from "solid-js";
import { type FormProblem, separator } from "../lib/form_problems";
import Tooltip from "./ui/Tooltip";

// FormProblems is what a submit with gaps says (#2432): one sentence naming each
// field to fix, each name a button that moves the focus to its field. It reads
// "To create the rule, fill in Name, Reads and Fires when it reads." and goes the
// moment nothing fails. A form passes its problems (lib/form_problems) and what
// focusing a field does; FormActionBar (#2440) can lift it as it stands.
export default function FormProblems(p: {
  // The verb phrase the sentence starts with: "create the rule".
  action: string;
  problems: FormProblem[];
  onFocus: (key: string) => void;
}): JSX.Element {
  return (
    <Show when={p.problems.length > 0}>
      <div role="alert" data-testid="form-problems" class="alert alert-warning alert-soft text-sm">
        <span>
          {`To ${p.action}, fill in `}
          <For each={p.problems}>
            {(pr, i) => (
              <>
                <Tooltip type="button" class="link font-medium" content={pr.message} onClick={() => p.onFocus(pr.key)}>{pr.label}</Tooltip>
                {separator(i(), p.problems.length)}
              </>
            )}
          </For>
          .
        </span>
      </div>
    </Show>
  );
}
