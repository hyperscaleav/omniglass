import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { OP, opsFor, tokenToChip, chipGlyph, type FilterKey, type Chip, type OpKey } from "../lib/predicate";
import { Search, X } from "./icons";

// FilterBar: the keyboard-driven chip search. A staged pipeline over one input:
// type a key, pick an operator, pick/enter a value; each commit is a chip. The
// matching/predicate logic lives in lib/predicate (tested); this is the input +
// suggestion UI. It is a bespoke combobox (the key->op->value staging does not
// map onto a standard listbox), with role/aria hand-set; Kobalte is used for the
// modal widgets (Drawer/CommandPalette) where it fits cleanly.
//
// trailing flows the action rail into the same wrap row; bare drops the card
// chrome; clearable shows Clear at the line end when chips exist.
type Suggestion =
  | { kind: "key"; label: string; hint: string }
  | { kind: "op"; op: OpKey; glyph: string; label: string }
  | { kind: "value"; value: string; hint: string };

const GLYPH_RE = /^(!=|>=|<=|[~=≠^$>≥<≤])/;

export default function FilterBar<T>(props: {
  keys: FilterKey<T>[];
  rows: T[];
  chips: Chip[];
  onChips: (chips: Chip[]) => void;
  placeholder?: string;
  bare?: boolean;
  clearable?: boolean;
  trailing?: JSX.Element;
}) {
  const [text, setText] = createSignal("");
  const [open, setOpen] = createSignal(false);
  const [sel, setSel] = createSignal(-1);
  let inputRef: HTMLInputElement | undefined;

  const fallbackKey = () => (props.keys.find((k) => k.hint === "substring") ?? props.keys[0])?.key ?? "";
  const keyOf = (k: string) => props.keys.find((s) => s.key === k);

  const suggestions = createMemo<Suggestion[]>(() => {
    const t = text();
    const colon = t.indexOf(":");
    if (colon < 0) {
      const frag = t.trim().toLowerCase();
      return props.keys
        .filter((k) => k.key.toLowerCase().includes(frag))
        .map((k) => ({ kind: "key", label: `${k.key}:`, hint: k.hint ?? "" }) as Suggestion);
    }
    const spec = keyOf(t.slice(0, colon));
    if (!spec) return [];
    const rest = t.slice(colon + 1);
    const m = rest.match(GLYPH_RE);
    const frag = (m ? rest.slice(m[0].length) : rest).trim().toLowerCase();
    const all = spec.values ? spec.values(props.rows) : [];
    const valSugs: Suggestion[] = all
      .filter((v) => v.toLowerCase().includes(frag))
      .map((v) => ({ kind: "value", value: v, hint: spec.valueLabel ? spec.valueLabel(v) : "" }));
    if (m) return valSugs;
    const opSugs: Suggestion[] = opsFor(spec.type)
      .filter((op) => frag === "" || op.includes(frag) || OP[op].label.includes(frag))
      .map((op) => ({ kind: "op", op, glyph: OP[op].glyph, label: OP[op].label }));
    return [...opSugs, ...valSugs];
  });

  const reset = () => {
    setText("");
    setOpen(false);
    setSel(-1);
  };
  const commit = (raw: string) => {
    const c = tokenToChip(raw, props.keys, fallbackKey());
    if (c) props.onChips([...props.chips, c]);
    reset();
  };
  const accept = (i: number) => {
    const s = suggestions()[i];
    if (!s) return;
    const t = text();
    const colon = t.indexOf(":");
    if (s.kind === "key") {
      setText(s.label);
      setSel(-1);
      setOpen(true);
      inputRef?.focus();
      return;
    }
    const key = t.slice(0, colon);
    if (s.kind === "op") {
      setText(`${key}:${OP[s.op].token}`);
      setSel(-1);
      setOpen(true);
      inputRef?.focus();
      return;
    }
    const rest = t.slice(colon + 1);
    const m = rest.match(GLYPH_RE);
    commit(`${key}:${m ? m[0] : ""}${s.value}`);
    inputRef?.focus();
  };
  const onKeyDown = (e: KeyboardEvent) => {
    const sugs = suggestions();
    if (e.key === "ArrowDown" && sugs.length) {
      e.preventDefault();
      setOpen(true);
      setSel((sel() + 1) % sugs.length);
    } else if (e.key === "ArrowUp" && sugs.length) {
      e.preventDefault();
      setSel((sel() - 1 + sugs.length) % sugs.length);
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (sel() >= 0 && sugs[sel()]) accept(sel());
      else if (text().trim()) commit(text());
    } else if (e.key === "Tab" && sugs.length) {
      e.preventDefault();
      if (sugs.length === 1) accept(0);
      else {
        setOpen(true);
        setSel((sel() + (e.shiftKey ? -1 : 1) + sugs.length) % sugs.length);
      }
    } else if (e.key === "Escape") {
      reset();
    } else if (e.key === "Backspace" && text() === "" && props.chips.length) {
      e.preventDefault();
      props.onChips(props.chips.slice(0, -1));
    }
  };
  const cycleOp = (i: number) => {
    const c = props.chips[i];
    const spec = keyOf(c.key);
    const list = opsFor(spec ? spec.type : "string");
    const next = list[(list.indexOf(c.op) + 1) % list.length];
    props.onChips(props.chips.map((x, j) => (j === i ? { ...x, op: next } : x)));
  };
  const reEdit = (i: number) => {
    const c = props.chips[i];
    props.onChips(props.chips.filter((_, j) => j !== i));
    setText(`${c.key}:${OP[c.op].token}${c.values[0]}`);
    setOpen(true);
    inputRef?.focus();
  };

  return (
    <div
      class={props.bare ? "min-w-0 flex-[1_1_340px]" : "card border border-base-300 bg-base-200 px-2.5 py-2"}
      onClick={(e) => e.currentTarget === e.target && inputRef?.focus()}
    >
      <div class="flex flex-wrap items-center gap-1.5">
        <span class="mr-0.5 inline-flex text-base-content/40"><Search size={15} /></span>
        <For each={props.chips}>
          {(c, i) => (
            <span class="inline-flex items-center gap-1 rounded-field border border-base-300 bg-base-100 py-[3px] pl-2.5 pr-1 text-xs">
              <span class="text-base-content/50">{c.key}</span>
              <button class="font-data font-semibold text-primary" title="cycle operator" onClick={() => cycleOp(i())}>
                {chipGlyph(c.op)}
              </button>
              <button class="font-data font-medium" onClick={() => reEdit(i())}>{c.values.join("|")}</button>
              <button class="ml-px inline-flex text-base-content/40" aria-label="remove" onClick={() => props.onChips(props.chips.filter((_, j) => j !== i()))}>
                <X size={13} />
              </button>
            </span>
          )}
        </For>
        <div class="relative min-w-[120px] flex-1">
          <input
            ref={inputRef}
            type="text"
            class="w-full bg-transparent px-0.5 py-1 text-sm outline-none placeholder:text-base-content/40"
            value={text()}
            placeholder={props.chips.length ? "" : (props.placeholder ?? "filter: key, operator, value")}
            role="combobox"
            aria-expanded={open()}
            onInput={(e) => {
              setText(e.currentTarget.value);
              setOpen(true);
              setSel(e.currentTarget.value.trim() ? 0 : -1);
            }}
            onFocus={() => setOpen(true)}
            onBlur={() => setTimeout(() => setOpen(false), 140)}
            onKeyDown={onKeyDown}
          />
          <Show when={open() && suggestions().length > 0}>
            <ul role="listbox" class="absolute z-40 mt-1.5 max-h-72 w-[300px] overflow-auto rounded-box border border-base-300 bg-base-100 p-1.5 shadow-2xl">
              <For each={suggestions()}>
                {(s, i) => (
                  <li>
                    <button
                      role="option"
                      aria-selected={sel() === i()}
                      class="flex w-full items-center justify-between gap-3 rounded-field px-2 py-1.5 text-left text-sm"
                      classList={{ "bg-primary/15": sel() === i() }}
                      onMouseDown={(e) => {
                        e.preventDefault();
                        accept(i());
                      }}
                      onMouseEnter={() => setSel(i())}
                    >
                      <span class="inline-flex items-center gap-2" classList={{ "font-semibold": s.kind === "key" }}>
                        <Show when={s.kind === "op"}>
                          <span class="w-4 text-center font-data text-primary">{(s as { glyph: string }).glyph}</span>
                        </Show>
                        {s.kind === "value" ? s.value : s.label}
                      </span>
                      <span class="text-xs text-base-content/40">{s.kind === "key" ? s.hint : s.kind === "op" ? "operator" : s.hint}</span>
                    </button>
                  </li>
                )}
              </For>
            </ul>
          </Show>
        </div>
        <Show when={props.clearable && props.chips.length > 0}>
          <button class="btn btn-ghost btn-sm flex-none text-xs" onClick={() => props.onChips([])}>Clear</button>
        </Show>
        <Show when={props.trailing}>
          <div class="ml-auto flex flex-none items-center gap-1.5">{props.trailing}</div>
        </Show>
      </div>
    </div>
  );
}
