import { For, Show, createEffect, createSignal, createUniqueId, on, onCleanup, type JSX } from "solid-js";
import { Compartment, EditorState } from "@codemirror/state";
import { EditorView, hoverTooltip, keymap, placeholder as placeholderText } from "@codemirror/view";
import { HighlightStyle, StreamLanguage, syntaxHighlighting } from "@codemirror/language";
import { autocompletion, completionKeymap } from "@codemirror/autocomplete";
import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import { setDiagnostics } from "@codemirror/lint";
import { tags } from "@lezer/highlight";
import InfoTip from "./InfoTip";
import Eyebrow from "./Eyebrow";
import { caretOf, validateExpression, type ExpressionBinding, type ExpressionCheck, type ValidateExpression } from "../lib/expressions";
import { bindingAt, exprCompletions, exprTokens, toEditorDiagnostics, type Vocabulary } from "../lib/expressionEditor";
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
// sits on the field; the editor's content carries the accessible name (a
// textbox named by `label`). Tab is left to move focus on, so the field never
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
  // What the site lets the expression name, from the latest answer: kept
  // across checks, and asked for while the source is empty, so completion has
  // the site's words from the first one on.
  const [vocab, setVocab] = createSignal<Vocabulary | null>(null);
  const diagId = createUniqueId();
  let timer: ReturnType<typeof setTimeout> | undefined;
  let seq = 0;
  // The source the current check answered, so its faults are placed in that
  // text and not in whatever has been typed since.
  let checked = "";

  createEffect(
    on([() => p.value, () => p.sampleKey, () => JSON.stringify(p.inputs ?? null), () => p.ownerKind], ([source, key]) => {
      clearTimeout(timer);
      const n = ++seq;
      const empty = !source.trim();
      if (!key || empty) {
        setCheck(null);
        setFailed(null);
        p.onCheck?.(false);
        if (!key) return;
      } else p.onCheck?.(undefined);
      timer = setTimeout(async () => {
        try {
          const body: ValidateExpression = { site: p.site, source, key };
          if (p.inputs) body.inputs = p.inputs.filter((i) => i.as && i.key);
          if (p.ownerKind) body.owner_kind = p.ownerKind;
          const r = await validateExpression(body);
          if (n !== seq) return;
          setVocab({ bindings: r.bindings ?? [], builtins: r.builtins ?? [] });
          // An empty source is asked only for its vocabulary: its verdict
          // (not ok) is already given, and being empty is no diagnostic.
          if (empty) return;
          setFailed(null);
          checked = source;
          setCheck(r);
          p.onCheck?.(r.ok);
        } catch (e) {
          if (n !== seq || empty) return;
          setCheck(null);
          setFailed(describeError(e));
          p.onCheck?.(false);
        }
      }, p.debounce ?? 300);
    }),
  );
  onCleanup(() => clearTimeout(timer));

  const diagnostics = () => check()?.diagnostics ?? [];

  const host = (
    <div
      class="textarea textarea-bordered w-full px-2 py-1.5 font-data text-sm leading-snug"
      classList={{ "textarea-error": diagnostics().length > 0 }}
    />
  ) as HTMLDivElement;
  const aria = new Compartment();
  const ariaAttrs = () =>
    EditorView.contentAttributes.of({
      "aria-label": p.label,
      "aria-describedby": diagId,
      ...(diagnostics().length > 0 ? { "aria-invalid": "true" } : {}),
    });
  const view = new EditorView({
    parent: host,
    state: EditorState.create({
      doc: p.value,
      extensions: [
        history(),
        keymap.of([...completionKeymap, ...defaultKeymap, ...historyKeymap]),
        StreamLanguage.define(exprTokens),
        syntaxHighlighting(exprHighlight),
        autocompletion({ override: [exprCompletions(vocab)], icons: false }),
        hoverTooltip((v, pos) => {
          const b = bindingAt(v.state.doc.toString(), pos, vocab()?.bindings ?? []);
          return b ? { pos, above: true, create: () => ({ dom: bindingCard(b) }) } : null;
        }),
        EditorView.lineWrapping,
        placeholderText(p.placeholder ?? ""),
        aria.of(ariaAttrs()),
        editorTheme,
        EditorView.updateListener.of((u) => {
          const text = u.state.doc.toString();
          if (u.docChanged && text !== p.value) p.onInput(text);
        }),
      ],
    }),
  });
  onCleanup(() => view.destroy());

  // The form's value is the source of truth: a value set from outside (the
  // builder's expression, a reset) replaces the editor's.
  createEffect(
    on(
      () => p.value,
      (v) => {
        if (v !== view.state.doc.toString()) view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: v } });
      },
      { defer: true },
    ),
  );
  createEffect(() => view.dispatch({ effects: aria.reconfigure(ariaAttrs()) }));
  // Each answer's faults underlined where they sit. Typed past the source it
  // answered, the editor maps the marks it has until the next answer lands.
  createEffect(() => {
    const ds = diagnostics();
    if (ds.length > 0 && view.state.doc.toString() !== checked) return;
    view.dispatch(setDiagnostics(view.state, toEditorDiagnostics(checked, ds)));
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
                    <pre class="overflow-x-auto rounded bg-base-200 px-2 py-1 font-data text-[12px] leading-snug text-base-content/80">
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
      <Show when={b.doc}>
        <span class="text-[12px] text-base-content/80">{b.doc}</span>
      </Show>
    </div>
  ) as HTMLElement;
}

// The theme's colors, by token kind. Every color is a daisyUI theme variable,
// so the editor follows the console's light and dark themes with no copy of
// either palette.
const exprHighlight = HighlightStyle.define([
  { tag: tags.keyword, color: "var(--color-secondary)" },
  { tag: tags.atom, color: "var(--color-warning)" },
  { tag: tags.string, color: "var(--color-success)" },
  { tag: tags.number, color: "var(--color-info)" },
  { tag: tags.function(tags.variableName), color: "var(--color-accent)" },
  { tag: tags.variableName, color: "var(--color-base-content)" },
  { tag: tags.operator, color: "color-mix(in oklab, var(--color-base-content) 70%, transparent)" },
  { tag: tags.comment, color: "color-mix(in oklab, var(--color-base-content) 50%, transparent)", fontStyle: "italic" },
]);

// The editor sits inside a daisyUI textarea frame (the frame draws the border,
// the focus ring and the error state); the theme only fits CodeMirror's own
// parts to it and to the console's surfaces.
const editorTheme = EditorView.theme({
  "&": { backgroundColor: "transparent", color: "inherit", fontSize: "inherit" },
  "&.cm-focused": { outline: "none" },
  ".cm-scroller": { fontFamily: "var(--og-font-data)", lineHeight: "inherit", maxHeight: "11rem", overflow: "auto" },
  ".cm-content": { padding: "0", minHeight: "2.75em", caretColor: "var(--color-base-content)" },
  ".cm-line": { padding: "0" },
  ".cm-placeholder": { color: "color-mix(in oklab, var(--color-base-content) 40%, transparent)" },
  ".cm-lintRange-error": {
    backgroundImage: "none",
    textDecoration: "underline wavy var(--color-error)",
    textDecorationSkipInk: "none",
    textUnderlineOffset: "3px",
  },
  ".cm-tooltip": {
    backgroundColor: "var(--color-base-200)",
    color: "var(--color-base-content)",
    border: "1px solid var(--color-base-300)",
    borderRadius: "0.375rem",
    fontFamily: "var(--og-font-ui)",
  },
  ".cm-tooltip-autocomplete > ul": { fontFamily: "var(--og-font-data)", fontSize: "12px" },
  ".cm-tooltip-autocomplete > ul > li[aria-selected]": {
    backgroundColor: "var(--color-primary)",
    color: "var(--color-primary-content)",
  },
  ".cm-completionDetail": { opacity: "0.6", marginLeft: "0.75em", fontStyle: "normal" },
  ".cm-completionInfo": { maxWidth: "20rem", padding: "0.375rem 0.5rem", fontSize: "12px" },
  ".cm-diagnostic-error": { borderLeftColor: "var(--color-error)" },
});
