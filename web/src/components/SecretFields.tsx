import { For, Show, createSignal, type JSX } from "solid-js";
import { revealSecret, type SecretField } from "../lib/secrets";
import { describeError } from "../lib/format";
import { Eye, Check } from "./icons";

// SecretFields renders a secret's field list masked, with an audited reveal: a
// caller holding secret:reveal can decrypt the values in place (the server
// audits every decrypt) and copy any field. Non-secret fields are always
// plaintext and copyable; secret fields copy only once revealed. Shared by the
// Secrets directory detail and the component effective-secrets blade.
export default function SecretFields(props: { secretId: string; fields: SecretField[]; canReveal: boolean }): JSX.Element {
  const [plain, setPlain] = createSignal<Record<string, string> | null>(null);
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);
  const [copied, setCopied] = createSignal<string | null>(null);

  async function reveal() {
    setBusy(true);
    setErr(null);
    try {
      setPlain(await revealSecret(props.secretId));
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  async function copy(name: string, value: string) {
    await navigator.clipboard.writeText(value);
    setCopied(name);
    setTimeout(() => copied() === name && setCopied(null), 1500);
  }

  // The value to show for a field: the revealed plaintext once decrypted, else
  // its masked/plaintext display value.
  const shown = (f: SecretField): string => {
    const p = plain();
    return p && f.name in p ? p[f.name] : f.value;
  };
  // A field is copyable when we have a real value: a non-secret field always, a
  // secret field once revealed.
  const copyable = (f: SecretField): boolean => !f.secret || !!plain();

  return (
    <div class="flex flex-col gap-2">
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <div class="overflow-hidden rounded-box border border-base-300">
        <For each={props.fields}>
          {(f, i) => (
            <div class="flex min-h-10 items-center gap-2 px-3 text-sm" classList={{ "border-t border-base-300": i() > 0 }}>
              <span class="shrink-0 font-data text-base-content/60">{f.name}</span>
              <span class="min-w-0 flex-1 truncate text-right font-data" classList={{ "text-base-content/40": f.secret && !plain() }} title={plain() ? shown(f) : undefined}>{shown(f)}</span>
              <Show when={copyable(f)}>
                <button
                  class="btn btn-quiet btn-xs shrink-0 gap-1"
                  onClick={() => copy(f.name, shown(f))}
                  title={`Copy ${f.name}`}
                >
                  <Show when={copied() === f.name} fallback={<span>Copy</span>}>
                    <Check size={12} /> Copied
                  </Show>
                </button>
              </Show>
            </div>
          )}
        </For>
      </div>
      <Show when={props.canReveal && props.fields.some((f) => f.secret)}>
        <button
          class="btn btn-quiet btn-sm w-fit gap-1.5"
          onClick={() => (plain() ? setPlain(null) : reveal())}
          disabled={busy()}
        >
          <Eye size={14} /> {busy() ? "Revealing…" : plain() ? "Hide secret values" : "Reveal secret values"}
        </button>
      </Show>
      <span class="text-[11px] text-base-content/40">Secret fields are encrypted at rest; every reveal is audited.</span>
    </div>
  );
}
