import { Compartment, EditorState } from "@codemirror/state";
import { EditorView, hoverTooltip, keymap, placeholder as placeholderText, tooltips } from "@codemirror/view";
import { HighlightStyle, StreamLanguage, syntaxHighlighting } from "@codemirror/language";
import { autocompletion, completionKeymap } from "@codemirror/autocomplete";
import { defaultKeymap, history, historyKeymap } from "@codemirror/commands";
import { setDiagnostics } from "@codemirror/lint";
import { tags } from "@lezer/highlight";
import type { ExpressionBinding, ExpressionDiagnostic } from "../lib/expressions";
import { bindingAt, exprCompletions, exprTokens, toEditorDiagnostics, type Vocabulary } from "../lib/expressionEditor";

// The CodeMirror half of ExpressionField (#1000), the only module that loads
// CodeMirror. ExpressionField imports it on first render, so the editor is a
// chunk of its own that only a page with an expression site downloads, not
// part of every console load. It takes plain callbacks and hands back a
// handle, so the Solid component keeps all of the field's state.

export type EditorAria = { label: string; describedBy: string; invalid: boolean };

export type ExpressionEditor = {
  setDoc(text: string): void;
  setAria(a: EditorAria): void;
  // Underline the diagnostics an answer gave for `source`.
  setDiagnostics(source: string, ds: ExpressionDiagnostic[]): void;
  destroy(): void;
};

export function mountExpressionEditor(
  host: HTMLElement,
  o: {
    doc: string;
    placeholder: string;
    aria: EditorAria;
    vocab: () => Vocabulary | null;
    onInput: (text: string) => void;
    bindingCard: (b: ExpressionBinding) => HTMLElement;
  },
): ExpressionEditor {
  const aria = new Compartment();
  const ariaAttrs = (a: EditorAria) =>
    EditorView.contentAttributes.of({
      "aria-label": a.label,
      "aria-describedby": a.describedBy,
      ...(a.invalid ? { "aria-invalid": "true" } : {}),
    });
  const view = new EditorView({
    parent: host,
    state: EditorState.create({
      doc: o.doc,
      extensions: [
        history(),
        keymap.of([...completionKeymap, ...defaultKeymap, ...historyKeymap]),
        StreamLanguage.define(exprTokens),
        syntaxHighlighting(exprHighlight),
        autocompletion({ override: [exprCompletions(o.vocab)], icons: false }),
        hoverTooltip((v, pos) => {
          const b = bindingAt(v.state.doc.toString(), pos, o.vocab()?.bindings ?? []);
          return b ? { pos, above: true, create: () => ({ dom: o.bindingCard(b) }) } : null;
        }),
        // Tooltips (completion, hover) mount on the dialog the field sits in,
        // else the page, never inside the form's scrolling body, which would
        // clip them at its edge (the portal rule overlays follow).
        // They keep a margin from the window's edge, where a drawer's field
        // would otherwise put them flush.
        tooltips({
          parent: (host.closest('[role="dialog"]') as HTMLElement | null) ?? document.body,
          tooltipSpace: (v) => {
            const w = v.dom.ownerDocument.defaultView ?? window;
            return { top: 8, left: 8, bottom: w.innerHeight - 8, right: w.innerWidth - 8 };
          },
        }),
        EditorView.lineWrapping,
        placeholderText(o.placeholder),
        aria.of(ariaAttrs(o.aria)),
        editorTheme,
        EditorView.updateListener.of((u) => {
          if (u.docChanged) o.onInput(u.state.doc.toString());
        }),
      ],
    }),
  });
  return {
    setDoc(text) {
      if (text !== view.state.doc.toString()) view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: text } });
    },
    setAria(a) {
      view.dispatch({ effects: aria.reconfigure(ariaAttrs(a)) });
    },
    setDiagnostics(source, ds) {
      // Typed past the source the answer was for, the editor keeps mapping
      // the marks it has until the next answer lands.
      if (ds.length > 0 && view.state.doc.toString() !== source) return;
      view.dispatch(setDiagnostics(view.state, toEditorDiagnostics(source, ds)));
    },
    destroy: () => view.destroy(),
  };
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
  // No ligatures: the field teaches what to type, and JetBrains Mono would
  // draw != and == as single glyphs.
  ".cm-content": { padding: "0", minHeight: "2.75em", caretColor: "var(--color-base-content)", fontVariantLigatures: "none" },
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
  ".cm-tooltip.cm-tooltip-autocomplete > ul": { fontFamily: "var(--og-font-data)", fontSize: "12px", fontVariantLigatures: "none", padding: "0.25rem 0" },
  ".cm-tooltip.cm-tooltip-autocomplete > ul > li": { padding: "0.125rem 0.625rem" },
  ".cm-tooltip.cm-tooltip-autocomplete > ul > li[aria-selected]": {
    backgroundColor: "var(--color-primary)",
    color: "var(--color-primary-content)",
  },
  ".cm-completionDetail": { opacity: "0.6", marginLeft: "0.75em", fontStyle: "normal" },
  ".cm-completionInfo": { maxWidth: "20rem", padding: "0.375rem 0.5rem", fontSize: "12px" },
  ".cm-diagnostic-error": { borderLeftColor: "var(--color-error)" },
});
