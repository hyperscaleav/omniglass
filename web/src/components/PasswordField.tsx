import { createSignal, Show } from "solid-js";
import { RevealButton, CopyButton, GenerateButton } from "./InlineActions";
import { generatePassword } from "../lib/password";
import { passwordError } from "../lib/validate";

// PasswordField is the shared password input for the IAM forms (the New user modal
// and the change-password card): a masked input with a show/hide toggle, inline
// policy validation (length and not-containing-username, mirroring the server), and,
// when `generate` is set, a Generate action that fills a crypto-strong random
// password (kept masked, copied with the Copy button or revealed on demand with the
// toggle) plus a Copy button. The common-password denylist stays server-side, so a
// generated password always passes and a manually typed common one is caught on submit.
export default function PasswordField(props: {
  id: string;
  value: string;
  onInput: (value: string) => void;
  username?: string;
  autocomplete?: string;
  placeholder?: string;
  disabled?: boolean;
  required?: boolean;
  generate?: boolean;
  // A server-side policy error (e.g. the common-password denylist) to render inline
  // under the field, so a post-submit rejection reads like the client checks. The
  // live client error takes precedence, and the consumer clears this on input.
  serverError?: string | null;
}) {
  const [reveal, setReveal] = createSignal(false);
  const error = () => passwordError(props.value, props.username) ?? props.serverError ?? null;

  const doGenerate = () => {
    props.onInput(generatePassword());
    // Stay masked: the admin copies it with the Copy button, or reveals on demand
    // with the show/hide toggle, rather than having it shown by default.
  };
  const doCopy = async (): Promise<boolean> => {
    if (!props.value) return false;
    try {
      await navigator.clipboard.writeText(props.value);
      return true;
    } catch {
      // Clipboard unavailable (insecure context or denied); the revealed field still
      // lets the admin copy by hand.
      return false;
    }
  };

  return (
    <div>
      <div class="join w-full">
        <input
          id={props.id}
          type={reveal() ? "text" : "password"}
          autocomplete={props.autocomplete ?? "new-password"}
          class="input input-bordered join-item w-full font-data"
          classList={{ "input-error": !!error() }}
          value={props.value}
          placeholder={props.placeholder}
          onInput={(e) => props.onInput(e.currentTarget.value)}
          disabled={props.disabled}
          required={props.required}
        />
        <RevealButton revealed={reveal()} onToggle={() => setReveal((r) => !r)} label="password" disabled={props.disabled} />
        <Show when={props.generate}>
          <CopyButton onCopy={doCopy} label="password" disabled={props.disabled || !props.value} />
          <GenerateButton onGenerate={doGenerate} label="Generate a strong password" disabled={props.disabled} />
        </Show>
      </div>
      <Show when={error()}>{(msg) => <p class="mt-1 text-[11px] text-error">{msg()}</p>}</Show>
    </div>
  );
}
