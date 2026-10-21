import { For, Show, createEffect, createSignal, createUniqueId, on, onCleanup, type JSX } from "solid-js";
import InfoTip from "./InfoTip";
import Eyebrow from "./Eyebrow";
import { caretOf, validateExpression, type ExpressionCheck, type ValidateExpression } from "../lib/expressions";
import { describeError } from "../lib/format";

// ExpressionField is the console's one expression editor (#960): the source in a
// monospace textarea, checked against its site as it is typed (POST
// /expressions:validate, debounced), with each diagnostic pointed at under its
// line and column the way a compiler points, and the site's bindings and
// functions listed from the same answer, each with its doc a hover away. The
// lists come from the server's declaration of the site, so the field teaches
// exactly the language the engine runs, never a copy of it.
//
// It renders the textarea FIRST and the rest beside it, so a FieldRow labels the
// textarea itself. No native validation attributes (validation-guard): the
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
  const diagId = createUniqueId();
  let timer: ReturnType<typeof setTimeout> | undefined;
  let seq = 0;

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
          const body: ValidateExpression = { site: p.site, source, key };
          if (p.inputs) body.inputs = p.inputs.filter((i) => i.as && i.key);
          if (p.ownerKind) body.owner_kind = p.ownerKind;
          const r = await validateExpression(body);
          if (n !== seq) return;
          setFailed(null);
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
  onCleanup(() => clearTimeout(timer));

  const diagnostics = () => check()?.diagnostics ?? [];
  const lines = () => Math.min(8, Math.max(2, p.value.split("\n").length));

  return (
    <>
      <textarea
        aria-label={p.label}
        aria-invalid={diagnostics().length > 0 ? "true" : undefined}
        aria-describedby={diagId}
        class="textarea textarea-bordered w-full font-data text-sm leading-snug"
        classList={{ "textarea-error": diagnostics().length > 0 }}
        rows={lines()}
        spellcheck={false}
        autocomplete="off"
        placeholder={p.placeholder}
        value={p.value}
        onInput={(e) => p.onInput(e.currentTarget.value)}
      />
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
      <Show when={check()}>
        {(c) => (
          <div class="flex flex-col gap-1.5 rounded border border-base-300 px-2 py-1.5">
            <Eyebrow
              label="It may name"
              hint="The bindings this expression can read, declared by the site and typed by what the rule reads."
            />
            <div data-testid="expression-bindings" class="flex flex-wrap gap-1.5">
              <For each={c().bindings ?? []}>
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
              <For each={c().builtins ?? []}>{(f) => <span>{f}</span>}</For>
            </div>
          </div>
        )}
      </Show>
    </>
  );
}
