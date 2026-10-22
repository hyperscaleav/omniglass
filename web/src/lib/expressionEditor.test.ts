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

// A system calc site's vocabulary: a list of records and each record's fields,
// which the server names with [] (members[].value.t).
const members: Vocabulary = {
  bindings: [
    { name: "members", type: "[]record", doc: "The system's members" },
    { name: "members[].name", type: "string", doc: "The member's name" },
    { name: "members[].value.t", type: "float64", doc: "The member's temperature" },
    { name: "members[].has.t", type: "bool" },
  ],
  builtins: ["all", "count"],
};

function completeIn(v: Vocabulary, doc: string, explicit = false) {
  const ctx = new CompletionContext(EditorState.create({ doc }), doc.length, explicit);
  return exprCompletions(() => v)(ctx) as { from: number; options: { label: string; type?: string }[] } | null;
}

describe("exprCompletions over a list of records", () => {
  it("offers the list by its name and never a [] path, which is not Expr", () => {
    const r = completeIn(members, "count(mem")!;
    expect(r.options.map((o) => o.label)).toEqual(["members", "all", "count"]);
  });

  it("offers a record's fields after a dot, as a predicate or an index reads them", () => {
    for (const doc of ["all(members, .ha", "count(members, #.va", "members[0].na", "all(members, ."]) {
      const r = completeIn(members, doc)!;
      expect(r.options.map((o) => [o.label, o.type])).toEqual([
        ["name", "property"],
        ["value.t", "property"],
        ["has.t", "property"],
      ]);
      expect(doc.slice(r.from)).toBe(doc.endsWith(".") ? "" : doc.slice(doc.lastIndexOf(".") + 1));
    }
  });
});

describe("bindingAt", () => {
  it("finds the binding whose name the position sits in, dotted names whole", () => {
    const doc = "value > 1 && component.name != ''";
    expect(bindingAt(doc, 2, vocab.bindings)?.name).toBe("value");
    expect(bindingAt(doc, doc.indexOf("name"), vocab.bindings)?.name).toBe("component.name");
    expect(bindingAt(doc, doc.indexOf(">"), vocab.bindings)).toBeNull();
  });

  it("names a record's field read after a dot by its full path", () => {
    expect(bindingAt("all(members, .has.t)", 15, members.bindings)?.name).toBe("members[].has.t");
    expect(bindingAt("members[0].value.t > 2", 12, members.bindings)?.name).toBe("members[].value.t");
    expect(bindingAt("count(members, .x)", 9, members.bindings)?.name).toBe("members");
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

  it("counts the column in characters, as Expr does, not in UTF-16 units", () => {
    const doc = 'value == "\u{1F525}" && humidity > 2';
    const col = Array.from(doc).indexOf("h") + 1;
    const [d] = toEditorDiagnostics(doc, [{ line: 1, column: col, phase: "type", message: "unknown name humidity" }]);
    expect(doc.slice(d.from, d.to)).toBe("humidity");
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

// A calc rule's vocabulary (#1018): its aliases as lists, the list
// functions, and on a system the members' record fields.
describe("exprCompletions over a calc rule's vocabulary", () => {
  const calc: Vocabulary = {
    bindings: [
      { name: "tcp_connect_time", type: "[]float64", doc: "tcp-connect-time: every member's current values" },
      { name: "members", type: "[]record" },
      { name: "members[].name", type: "string" },
      { name: "members[].value.tcp_connect_time", type: "float64" },
      { name: "members[].has.tcp_connect_time", type: "bool" },
    ],
    builtins: ["all", "any", "avg", "count", "len", "majority", "max", "min", "sum"],
  };
  const done = (doc: string) => {
    const ctx = new CompletionContext(EditorState.create({ doc }), doc.length, false);
    return (exprCompletions(() => calc)(ctx) as { options: { label: string }[] } | null)?.options.map((o) => o.label) ?? [];
  };

  it("completes an alias inside a list function's slot, the functions, and a member's fields", () => {
    expect(done("max(tc")).toContain("tcp_connect_time");
    expect(done("av")).toContain("avg");
    expect(done("maj")).toContain("majority");
    expect(done("count(members, .")).toEqual(["name", "value.tcp_connect_time", "has.tcp_connect_time"]);
  });
});

describe("toEditorDiagnostics' bind action", () => {
  it("offers to bind a name nothing binds, and only that", () => {
    const bound: string[] = [];
    const [d] = toEditorDiagnostics("max(fan)", [{ line: 1, column: 5, phase: "type", message: "fan is not an input", unbound: "fan" }], (n) => bound.push(n));
    expect(d.actions?.map((a) => a.name)).toEqual(["Bind fan to a key"]);
    d.actions![0].apply(null as never, d.from, d.to);
    expect(bound).toEqual(["fan"]);
    const [plain] = toEditorDiagnostics("max(x)", [{ line: 1, column: 1, phase: "type", message: "expected bool" }], () => {});
    expect(plain.actions).toBeUndefined();
  });
});
