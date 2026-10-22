import { For, Show, createEffect, createSignal, createUniqueId, on, onCleanup, untrack, type JSX } from "solid-js";
import InfoTip from "./InfoTip";
import Eyebrow from "./Eyebrow";
import { caretOf, validateExpression, type ExpressionBinding, type ExpressionCheck, type ValidateExpression } from "../lib/expressions";
import type { Vocabulary } from "../lib/expressionEditor";
import type { ExpressionEditor } from "./expressionEditorView";
import { describeError } from "../lib/format";

// ExpressionField is the console's one expression editor (#960, #1000): the
// source in a CodeMirror editor that reads Expr (highlighting), completes the
// site's bindings and builtins, shows a binding's doc on hover, and underlines
// each fault where it sits, all from one answer, POST /expressions:validate
// for the site, checked as it is typed (debounced). Beneath it each diagnostic
// is also pointed at under its line and column the way a compiler points, and
// the site's bindings and functions are listed, each with its doc a hover
// away. Everything it offers comes from the server's declaration of the site,
// so the field teaches exactly the language the engine runs, never a copy.
//
// It renders the editor FIRST and the rest beside it, so a FieldRow's label
// is the field's: a click on it focuses the editor, and the editor's content
// carries the accessible name (a textbox named by `label`). Tab is left to move focus on, so the field never
// traps the keyboard. No native validation attributes (validation-guard): the
// check is the server's, and the caller learns the verdict through onCheck.
export default function ExpressionField(p: {
  site: ValidateExpression["site"];
  // The sample key the expression reads (criteria types its value by it); the
  // field checks nothing until there is one.
  sampleKey: string;
  value: string;
  onInput: (v: string) => void;
  label: string;
  placeholder?: string;
  // How long typing must pause before a check (ms).
  debounce?: number;
  // For the calc site (#999): the rule's inputs and owner kind, which give the
  // expression its bindings (sampleKey is then the output key, which types the
  // result).
  inputs?: { as: string; key: string }[];
  ownerKind?: "component" | "system";
  // What to say while there is no key to check against.
  needsKey?: string;
  // The verdict, so a form can refuse a source that does not compile: true
  // when it compiles, false when it does not (or is empty, has no key to be
  // checked against, or could not be checked), undefined while a check is
  // pending.
  onCheck?: (ok: boolean | undefined) => void;
}): JSX.Element {
  const [check, setCheck] = createSignal<ExpressionCheck | null>(null);
  const [failed, setFailed] = createSignal<string | null>(null);
  // What the site lets the expression name (its bindings and builtins),
  // fetched apart from the source check, so completion has the site's words
  // from the first one on.
  const [vocab, setVocab] = createSignal<Vocabulary | null>(null);
  const diagId = createUniqueId();
  let timer: ReturnType<typeof setTimeout> | undefined;
  let vocabTimer: ReturnType<typeof setTimeout> | undefined;
  let seq = 0;
  let vocabSeq = 0;
  // The source the current check answered, so its faults are placed in that
  // text and not in whatever has been typed since.
  let checked = "";

  const request = (source: string, key: string): ValidateExpression => {
    const body: ValidateExpression = { site: p.site, source, key };
    if (p.inputs) body.inputs = p.inputs.filter((i) => i.as && i.key);
    if (p.ownerKind) body.owner_kind = p.ownerKind;
    return body;
  };

  // The vocabulary depends on the site, the key and the calc inputs, never on
  // the source, so it is asked for (with an empty source) whenever those
  // change, on its own clock: typing, which holds the source check back
  // until a pause, never holds back the words the first one completes from.
  createEffect(
    on([() => p.sampleKey, () => JSON.stringify(p.inputs ?? null), () => p.ownerKind], ([key]) => {
      clearTimeout(vocabTimer);
      const n = ++vocabSeq;
      if (!key) {
        setVocab(null);
        return;
      }
      vocabTimer = setTimeout(async () => {
        try {
          const r = await validateExpression(request("", key));
          if (n === vocabSeq) setVocab({ bindings: r.bindings ?? [], builtins: r.builtins ?? [] });
        } catch {
          // Refused (a calc input the server no longer accepts, say): no
          // vocabulary, so nothing offers a name the engine has just refused.
          // The source check reports why.
          if (n === vocabSeq) setVocab(null);
        }
      }, p.debounce ?? 300);
    }),
  );

  createEffect(
    on([() => p.value, () => p.sampleKey, () => JSON.stringify(p.inputs ?? null), () => p.ownerKind], ([source, key]) => {
      clearTimeout(timer);
      const n = ++seq;
      if (!key || !source.trim()) {
        setCheck(null);
        setFailed(null);
        p.onCheck?.(false);
        return;
      }
      p.onCheck?.(undefined);
      timer = setTimeout(async () => {
        try {
          const r = await validateExpression(request(source, key));
          if (n !== seq) return;
          setFailed(null);
          checked = source;
          setCheck(r);
          p.onCheck?.(r.ok);
        } catch (e) {
          if (n !== seq) return;
          setCheck(null);
          setFailed(describeError(e));
          p.onCheck?.(false);
        }
      }, p.debounce ?? 300);
    }),
  );
  onCleanup(() => {
    clearTimeout(timer);
    clearTimeout(vocabTimer);
  });

  const diagnostics = () => check()?.diagnostics ?? [];

  const host = (
    <div
      class="textarea textarea-bordered min-h-0 w-full px-2 py-1.5 font-data text-sm leading-snug"
      classList={{ "textarea-error": diagnostics().length > 0 }}
    />
  ) as HTMLDivElement;
  const aria = () => ({ label: p.label, describedBy: diagId, invalid: diagnostics().length > 0 });
  const [editor, setEditor] = createSignal<ExpressionEditor | null>(null);
  let gone = false;
  // A FieldRow's <label for> points at the host, which is not labelable, so
  // a click on the label focuses the editor by hand (as it did the textarea).
  let label: Element | null = null;
  const focusEditor = () => editor()?.focus();
  onCleanup(() => {
    gone = true;
    label?.removeEventListener("click", focusEditor);
    editor()?.destroy();
  });
  // CodeMirror is its own chunk, loaded the first time a field renders.
  void import("./expressionEditorView").then(({ mountExpressionEditor }) => {
    if (gone) return;
    setEditor(
      mountExpressionEditor(host, {
        doc: untrack(() => p.value),
        placeholder: untrack(() => p.placeholder ?? ""),
        aria: untrack(aria),
        vocab,
        onInput: (text) => {
          if (text !== p.value) p.onInput(text);
        },
        bindingCard,
      }),
    );
    label = host.id ? document.querySelector(`label[for="${host.id}"]`) : null;
    label?.addEventListener("click", focusEditor);
  });

  // The form's value is the source of truth: a value set from outside (the
  // builder's expression, a reset) replaces the editor's.
  createEffect(() => editor()?.setDoc(p.value));
  createEffect(() => editor()?.setAria(aria()));
  // Each answer's faults underlined in the source it answered.
  createEffect(() => {
    const ds = diagnostics();
    editor()?.setDiagnostics(checked, ds);
  });

  return (
    <>
      {host}
      <div id={diagId} role="status" aria-live="polite" class="flex flex-col gap-1">
        <Show when={!p.sampleKey}>
          <span class="text-[11px] text-base-content/50">{p.needsKey ?? "Choose what the rule reads to check this expression."}</span>
        </Show>
        <Show when={failed()}>{(m) => <span class="text-[11px] text-error">{m()}</span>}</Show>
        <For each={diagnostics()}>
          {(d) => {
            const at = caretOf(p.value, d.line, d.column);
            return (
              <div data-testid="expression-diagnostic" class="flex flex-col gap-0.5">
                <Show when={at}>
                  {(a) => (
                    <pre class="overflow-x-auto rounded bg-base-200 px-2 py-1 font-data text-[12px] leading-snug text-base-content/80 [font-variant-ligatures:none]">
                      {`${a().text}\n${a().caret}`}
                    </pre>
                  )}
                </Show>
                <span class="text-[11px] text-error">
                  {d.line > 0 ? `Line ${d.line}, column ${d.column}: ` : ""}
                  {d.message}
                </span>
              </div>
            );
          }}
        </For>
        <Show when={check()?.ok}>
          <span class="text-[11px] text-success" data-testid="expression-ok">Compiles; yields {check()!.result_type}.</span>
        </Show>
      </div>
      <Show when={vocab()}>
        {(c) => (
          <div class="flex flex-col gap-1.5 rounded border border-base-300 px-2 py-1.5">
            <Eyebrow
              label="It may name"
              hint="The bindings this expression can read, declared by the site and typed by what the rule reads. Ctrl-Space in the field offers them."
            />
            <div data-testid="expression-bindings" class="flex flex-wrap gap-1.5">
              <For each={c().bindings}>
                {(b) => (
                  <span class="inline-flex items-center gap-1">
                    <code class="font-data text-[12px]">{b.name}</code>
                    <span class="text-[11px] text-base-content/50">{b.type}</span>
                    <Show when={b.doc}>
                      <InfoTip label={b.name} text={b.doc!} />
                    </Show>
                  </span>
                )}
              </For>
            </div>
            <Eyebrow label="Functions" hint="The builtins this site admits; any other function is refused when the rule is saved." />
            <div data-testid="expression-builtins" class="flex flex-wrap gap-x-2 gap-y-0.5 font-data text-[12px] text-base-content/70">
              <For each={c().builtins}>{(f) => <span>{f}</span>}</For>
            </div>
          </div>
        )}
      </Show>
    </>
  );
}

// The hover card for a binding: its name and type, and what it holds.
function bindingCard(b: ExpressionBinding): HTMLElement {
  return (
    <div class="flex max-w-xs flex-col gap-0.5 px-2 py-1.5">
      <span class="inline-flex items-baseline gap-1.5">
        <code class="font-data text-[12px]">{b.name}</code>
        <span class="text-[11px] text-base-content/50">{b.type}</span>
      </span>
      {b.doc ? <span class="text-[12px] text-base-content/80">{b.doc}</span> : null}
    </div>
  ) as HTMLElement;
}
