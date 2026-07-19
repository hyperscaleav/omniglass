import { For, Show, createSignal, type JSX } from "solid-js";
import { revealSecret, copySecret, type SecretField } from "../lib/secrets";
import { describeError } from "../lib/format";
import { RevealButton, CopyButton } from "./InlineActions";

// SecretFields renders a secret's fields as read-only value inputs with in-field
// adornments, matching the PasswordField pattern: a secret field carries an eye
// toggle (reveal) and a copy button, both of which decrypt through the audited
// reveal endpoint, so revealing OR copying a secret value writes an audit row.
// A non-secret field is plaintext and copies client-side (no decrypt, no audit).
// Used by the Secrets directory detail blade.
export default function SecretFields(props: { secretId: string; fields: SecretField[]; canReveal: boolean }): JSX.Element {
  // Per-field revealed plaintext, populated only after an audited decrypt.
  const [shown, setShown] = createSignal<Record<string, string>>({});
  const [busy, setBusy] = createSignal<string | null>(null);
  const [err, setErr] = createSignal<string | null>(null);

  // Decrypt through the audited endpoints: the eye reveals (verb `reveal`), the
  // copy decrypts for the clipboard (verb `copy`), so the audit tells them apart.
  const revealPlain = () => revealSecret(props.secretId);
  const copyPlain = () => copySecret(props.secretId);

  async function toggleReveal(name: string) {
    if (shown()[name] !== undefined) {
      setShown((s) => { const c = { ...s }; delete c[name]; return c; });
      return;
    }
    setBusy(name);
    setErr(null);
    try {
      const plain = await revealPlain();
      setShown((s) => ({ ...s, [name]: plain[name] ?? "" }));
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(null);
    }
  }

  async function copyField(f: SecretField): Promise<boolean> {
    setErr(null);
    try {
      // A secret field decrypts through the audited copy endpoint on every copy;
      // a non-secret field is already plaintext.
      const value = f.secret ? (await copyPlain())[f.name] ?? "" : f.value;
      await navigator.clipboard.writeText(value);
      return true;
    } catch (e) {
      setErr(describeError(e));
      return false;
    }
  }

  const display = (f: SecretField): string => shown()[f.name] ?? f.value;
  const canCopy = (f: SecretField): boolean => !f.secret || props.canReveal;

  return (
    <div class="flex flex-col gap-3">
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <For each={props.fields}>
        {(f) => (
          <label class="flex flex-col gap-1">
            <span class="text-[11px] uppercase tracking-wide text-base-content/40">{f.name}</span>
            <div class="join w-full">
              <input
                readonly
                class="input input-bordered join-item w-full font-data text-sm"
                classList={{ "text-base-content/50": f.secret && shown()[f.name] === undefined }}
                value={display(f)}
                title={shown()[f.name] ?? undefined}
              />
              <Show when={f.secret && props.canReveal}>
                <RevealButton
                  revealed={shown()[f.name] !== undefined}
                  onToggle={() => toggleReveal(f.name)}
                  label={f.name}
                  disabled={busy() === f.name}
                />
              </Show>
              <Show when={canCopy(f)}>
                <CopyButton onCopy={() => copyField(f)} label={f.name} />
              </Show>
            </div>
          </label>
        )}
      </For>
      <span class="text-[11px] text-base-content/40">Secret fields are encrypted at rest; revealing or copying one is audited.</span>
    </div>
  );
}
