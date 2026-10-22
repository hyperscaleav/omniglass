import { EditorView } from "@codemirror/view";

// Test helpers for the expression editor (#1000): CodeMirror owns its
// content, so a test reads and types through the editor's own state rather
// than a textarea's value. Both take the element the field's label names
// (the editor's content, role textbox).

export function expressionText(el: Element): string {
  return EditorView.findFromDOM(el as HTMLElement)!.state.doc.toString();
}

// typeExpression replaces the source as the author typing it would.
export function typeExpression(el: Element, text: string): void {
  const view = EditorView.findFromDOM(el as HTMLElement)!;
  view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: text }, userEvent: "input.type" });
}
