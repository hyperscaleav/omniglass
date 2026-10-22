import { describe, it, expect } from "vitest";
import { EditorState } from "@codemirror/state";
import { CompletionContext } from "@codemirror/autocomplete";
import { StringStream } from "@codemirror/language";
import { bindingAt, exprCompletions, exprTokens, toEditorDiagnostics, type Vocabulary } from "./expressionEditor";

const vocab: Vocabulary = {
  bindings: [
    { name: "value", type: "float64", doc: "The sample's value" },
    { name: "component.name", type: "string", doc: "The component's name" },
    { name: "component.tags", type: "[]string" },
  ],
  builtins: ["abs", "len", "all"],
};

// Every token the highlighter gives a line, as [text, style] pairs.
function tokens(line: string): [string, string | null][] {
  const state = exprTokens.startState!(2);
  const s = new StringStream(line, 2, 2);
  const out: [string, string | null][] = [];
  while (!s.eol()) {
    s.start = s.pos;
    const style = exprTokens.token(s, state);
    if (s.pos === s.start) s.next();
    const text = line.slice(s.start, s.pos);
    if (text.trim()) out.push([text, style]);
  }
  return out;
}

function complete(doc: string, pos = doc.length, explicit = false) {
  const ctx = new CompletionContext(EditorState.create({ doc }), pos, explicit);
  return exprCompletions(() => vocab)(ctx) as { from: number; options: { label: string; type?: string; detail?: string }[] } | null;
}

describe("exprTokens", () => {
  it("tells Expr's keywords, words, literals and operators apart", () => {
    expect(tokens('value > 40 && not (component.name in ["a", \'b\'])')).toEqual([
      ["value", "variableName"],
      [">", "operator"],
      ["40", "number"],
      ["&&", "operator"],
      ["not", "keyword"],
      ["(", "paren"],
      ["component.name", "variableName"],
      ["in", "keyword"],
      ["[", "squareBracket"],
      ['"a"', "string"],
      [",", "punctuation"],
      ["'b'", "string"],
      ["]", "squareBracket"],
      [")", "paren"],
    ]);
  });

  it("reads a call as a function, and true, false and nil as atoms", () => {
    expect(tokens("len(x) == 0 or true or nil")).toEqual([
      ["len", "variableName.function"],
      ["(", "paren"],
      ["x", "variableName"],
      [")", "paren"],
      ["==", "operator"],
      ["0", "number"],
      ["or", "keyword"],
      ["true", "atom"],
      ["or", "keyword"],
      ["nil", "atom"],
    ]);
  });

  it("reads an escaped quote inside a string, decimals, and a comment", () => {
    expect(tokens('"a\\"b" + 1.5e3 // why')).toEqual([
      ['"a\\"b"', "string"],
      ["+", "operator"],
      ["1.5e3", "number"],
      ["// why", "comment"],
    ]);
  });
});

describe("exprCompletions", () => {
  it("offers exactly the site's bindings and builtins, each with its kind", () => {
    const r = complete("", 0, true)!;
    expect(r.from).toBe(0);
    expect(r.options.map((o) => [o.label, o.type])).toEqual([
      ["value", "variable"],
      ["component.name", "variable"],
      ["component.tags", "variable"],
      ["abs", "function"],
      ["len", "function"],
      ["all", "function"],
    ]);
    expect(r.options[0].detail).toBe("float64");
  });

  it("completes the dotted word under the cursor from its start", () => {
    const r = complete("value > 1 && component.na")!;
    expect(r.from).toBe("value > 1 && ".length);
  });

  it("stays quiet when nothing is typed and nobody asked, and inside a string", () => {
    expect(complete("value > ")).toBeNull();
    expect(complete('value == "comp')).toBeNull();
  });
});

describe("bindingAt", () => {
  it("finds the binding whose name the position sits in, dotted names whole", () => {
    const doc = "value > 1 && component.name != ''";
    expect(bindingAt(doc, 2, vocab.bindings)?.name).toBe("value");
    expect(bindingAt(doc, doc.indexOf("name"), vocab.bindings)?.name).toBe("component.name");
    expect(bindingAt(doc, doc.indexOf(">"), vocab.bindings)).toBeNull();
  });

  it("answers the record for a field path under it, and nothing for an unknown word", () => {
    expect(bindingAt("component.tags[0]", 3, vocab.bindings)?.name).toBe("component.tags");
    expect(bindingAt("humidity", 2, vocab.bindings)).toBeNull();
  });
});

describe("toEditorDiagnostics", () => {
  it("underlines the word at a diagnostic's line and column", () => {
    const doc = "value > 40 &&\n  humidity > 2";
    const [d] = toEditorDiagnostics(doc, [{ line: 2, column: 3, phase: "type", message: "unknown name humidity" }]);
    expect(doc.slice(d.from, d.to)).toBe("humidity");
    expect(d.severity).toBe("error");
    expect(d.message).toBe("unknown name humidity");
  });

  it("marks one character where no word starts, the end where the column runs past it, and skips a positionless one", () => {
    const doc = "value >";
    const ds = toEditorDiagnostics(doc, [
      { line: 1, column: 7, phase: "parse", message: "unexpected" },
      { line: 1, column: 9, phase: "parse", message: "unexpected end" },
      { line: 0, column: 0, phase: "limit", message: "too long" },
      { line: 4, column: 1, phase: "parse", message: "off the end" },
    ]);
    expect(ds.map((d) => [d.from, d.to])).toEqual([
      [6, 7],
      [7, 7],
    ]);
  });
});
