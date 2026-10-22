import type { StreamParser, StringStream } from "@codemirror/language";
import type { CompletionContext, CompletionResult, CompletionSource } from "@codemirror/autocomplete";
import type { Diagnostic } from "@codemirror/lint";
import type { ExpressionBinding, ExpressionDiagnostic } from "./expressions";

// The pure half of the expression editor (#1000): how CodeMirror reads Expr
// (the highlighter's tokens), what it may complete, which binding a hover
// names, and where a server diagnostic lands in the source. The vocabulary is
// always the server's answer for the site (POST /expressions:validate), never
// a list kept here, so the editor offers exactly what the engine compiles.

export type Vocabulary = { bindings: ExpressionBinding[]; builtins: string[] };

// Expr's word operators and its literal atoms. These are the language's own
// grammar, not a site's vocabulary, which is why they are the one list here.
const KEYWORDS = new Set(["and", "or", "not", "in", "matches", "contains", "startsWith", "endsWith", "let", "if", "else"]);
const ATOMS = new Set(["true", "false", "nil"]);
const BRACKETS: Record<string, string> = { "(": "paren", ")": "paren", "[": "squareBracket", "]": "squareBracket", "{": "brace", "}": "brace" };

function quoted(s: StringStream, q: string): string {
  let escaped = false;
  for (let c = s.next(); c !== undefined; c = s.next()) {
    if (c === q && !escaped) break;
    escaped = !escaped && c === "\\";
  }
  return "string";
}

// exprTokens is Expr as a CodeMirror stream language: each call consumes one
// token and names its highlight tag. A dotted path (component.name) is one
// word, the way a binding is named; a word followed by "(" is a call.
export const exprTokens: StreamParser<{ inComment: boolean }> = {
  name: "expr",
  startState: () => ({ inComment: false }),
  token(s, st) {
    if (st.inComment) {
      if (s.skipTo("*/")) {
        s.pos += 2;
        st.inComment = false;
      } else s.skipToEnd();
      return "comment";
    }
    if (s.eatSpace()) return null;
    if (s.match("//")) {
      s.skipToEnd();
      return "comment";
    }
    if (s.match("/*")) {
      st.inComment = true;
      return "comment";
    }
    const c = s.peek()!;
    if (c === '"' || c === "'" || c === "`") {
      s.next();
      return quoted(s, c);
    }
    if (s.match(/^(?:0x[0-9a-fA-F_]+|\d[\d_]*(?:\.\d+)?(?:[eE][+-]?\d+)?|\.\d+(?:[eE][+-]?\d+)?)/)) return "number";
    const word = s.match(/^[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)*/) as RegExpMatchArray | null;
    if (word) {
      const w = word[0];
      if (KEYWORDS.has(w)) return "keyword";
      if (ATOMS.has(w)) return "atom";
      return s.match(/^\s*\(/, false) ? "variableName.function" : "variableName";
    }
    if (BRACKETS[c]) {
      s.next();
      return BRACKETS[c];
    }
    if (c === "," || c === ";") {
      s.next();
      return "punctuation";
    }
    if (s.match(/^(?:\?\?|\?\.|\.\.|\*\*|==|!=|<=|>=|&&|\|\||[-+*/%<>!?:|.^=#])/)) return "operator";
    s.next();
    return null;
  },
};

// The word (a dotted path) the cursor is in or just after.
const WORD = /[A-Za-z_$][\w$.]*$/;

// The server names a list's element fields with [] (members[].value.t), a
// spelling that documents the shape but is not Expr: a source reads the list
// by its name and an element's fields after a dot, in a predicate
// (all(members, .has.t)) or past an index (members[0].name). So a vocabulary
// splits into the names a word may start with and the fields after a dot.
function split(bindings: ExpressionBinding[]): { roots: ExpressionBinding[]; fields: { field: string; b: ExpressionBinding }[] } {
  const roots: ExpressionBinding[] = [];
  const fields: { field: string; b: ExpressionBinding }[] = [];
  for (const b of bindings) {
    const at = b.name.indexOf("[].");
    if (at < 0) roots.push(b);
    else fields.push({ field: b.name.slice(at + 3), b });
  }
  return { roots, fields };
}

// exprCompletions completes the word before the cursor from the site's
// bindings and builtins: after a dot, an element's fields; otherwise the
// names and functions a word may start with. It offers nothing inside a
// string, and nothing unprompted until a word (or a field's dot) is begun;
// Ctrl-Space asks explicitly.
export function exprCompletions(vocab: () => Vocabulary | null): CompletionSource {
  return (ctx: CompletionContext): CompletionResult | null => {
    const v = vocab();
    if (!v) return null;
    const line = ctx.state.doc.lineAt(ctx.pos);
    const before = line.text.slice(0, ctx.pos - line.from);
    if (insideString(before)) return null;
    const m = WORD.exec(before);
    const from = m ? ctx.pos - m[0].length : ctx.pos;
    const afterDot = m ? before[m.index - 1] === "." : before.endsWith(".");
    const { roots, fields } = split(v.bindings);
    if (afterDot) {
      if (fields.length === 0) return null;
      return {
        from,
        options: fields.map(({ field, b }) => ({ label: field, type: "property", detail: b.type, info: b.doc || undefined })),
        validFor: /^[\w$.]*$/,
      };
    }
    if (!m && !ctx.explicit) return null;
    return {
      from,
      options: [
        // The site's names lead the functions: in a function's empty slot
        // (a template's) the alias to fold is the likeliest next word.
        ...roots.map((b) => ({ label: b.name, type: "variable", detail: b.type, info: b.doc || undefined, boost: 1 })),
        ...v.builtins.map((f) => ({ label: f, type: "function" })),
      ],
      validFor: /^[\w$.]*$/,
    };
  };
}

function insideString(before: string): boolean {
  let q: string | null = null;
  let escaped = false;
  for (const c of before) {
    if (q) {
      if (c === q && !escaped) q = null;
      escaped = !escaped && c === "\\";
    } else if (c === '"' || c === "'" || c === "`") q = c;
  }
  return q !== null;
}

// bindingAt is the binding the word at pos names: the dotted path under the
// position, or the longest binding it extends (component.tags for
// component.tags.x), read as an element's field when a dot comes before it
// (.has.t names members[].has.t); null over anything that is not a binding.
export function bindingAt(doc: string, pos: number, bindings: ExpressionBinding[]): ExpressionBinding | null {
  const isWord = (c: string | undefined) => !!c && /[\w$.]/.test(c);
  let from = pos;
  let to = pos;
  while (from > 0 && isWord(doc[from - 1])) from--;
  while (to < doc.length && isWord(doc[to])) to++;
  let word = doc.slice(from, to);
  const { roots, fields } = split(bindings);
  // A path that starts with a dot (.has.t, #.has.t, members[0].name) is an
  // element's field.
  const field = word.startsWith(".");
  word = word.replace(/^\.+/, "");
  if (!word) return null;
  const named = field ? fields.map(({ field: f, b }) => ({ name: f, b })) : roots.map((b) => ({ name: b.name, b }));
  let best: { name: string; b: ExpressionBinding } | null = null;
  for (const n of named) {
    if ((word === n.name || word.startsWith(n.name + ".")) && (!best || n.name.length > best.name.length)) best = n;
  }
  return best?.b ?? null;
}

// bindLabel is the action that binds an unbound name.
export const bindLabel = (name: string) => `Bind ${name} to a key`;

// toEditorDiagnostics places the server's diagnostics in the source: each at
// its 1-based line and column (Expr counts the column in characters, so it is
// walked by code point, never indexed as UTF-16), underlining the word that starts there (one
// character where none does, or the line's end when the column runs past
// it). A diagnostic with no position (a length cap), or one off the end of
// the source, is left to the list beneath the field.
//
// A diagnostic that names a name nothing binds (unbound) carries an action
// to bind it, when onBind is given (a calc rule binds a name by giving an
// input that alias).
export function toEditorDiagnostics(doc: string, diags: ExpressionDiagnostic[], onBind?: (name: string) => void): Diagnostic[] {
  const lines = doc.split("\n");
  const out: Diagnostic[] = [];
  for (const d of diags) {
    if (d.line < 1 || d.column < 1 || d.line > lines.length) continue;
    let start = 0;
    for (let i = 0; i < d.line - 1; i++) start += lines[i].length + 1;
    const text = lines[d.line - 1];
    const chars = Array.from(text);
    const col = chars.slice(0, Math.min(d.column - 1, chars.length)).join("").length;
    const word = /^[\w$.]+/.exec(text.slice(col));
    const len = word ? word[0].length : col < text.length ? 1 : 0;
    const unbound = d.unbound;
    out.push({
      from: start + col, to: start + col + len, severity: "error", message: d.message, source: d.phase,
      ...(unbound && onBind ? { actions: [{ name: bindLabel(unbound), apply: () => onBind(unbound) }] } : {}),
    });
  }
  return out;
}
