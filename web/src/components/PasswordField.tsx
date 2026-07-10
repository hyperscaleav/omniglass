import { createSignal, Show } from "solid-js";
import { Eye, EyeOff, Copy, Check, RefreshCw } from "./icons";
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
  const [copied, setCopied] = createSignal(false);
  const error = () => passwordError(props.value, props.username) ?? props.serverError ?? null;

  const doGenerate = () => {
    props.onInput(generatePassword());
    // Stay masked: the admin copies it with the Copy button, or reveals on demand
    // with the show/hide toggle, rather than having it shown by default.
  };
  const doCopy = async () => {
    if (!props.value) return;
    try {
      await navigator.clipboard.writeText(props.value);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard unavailable (insecure context or denied); the revealed field still
      // lets the admin copy by hand.
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
        <button
          type="button"
          class="btn btn-bordered join-item btn-square"
          aria-label={reveal() ? "Hide password" : "Show password"}
          onClick={() => setReveal((r) => !r)}
          disabled={props.disabled}
        >
          <Show when={reveal()} fallback={<Eye size={15} />}><EyeOff size={15} /></Show>
        </button>
        <Show when={props.generate}>
          <button
            type="button"
            class="btn btn-bordered join-item btn-square"
            aria-label="Copy password"
            title="Copy"
            onClick={doCopy}
            disabled={props.disabled || !props.value}
          >
            <Show when={copied()} fallback={<Copy size={15} />}><Check size={15} /></Show>
          </button>
          <button
            type="button"
            class="btn btn-bordered join-item btn-square"
            aria-label="Generate a strong password"
            title="Generate a strong password"
            onClick={doGenerate}
            disabled={props.disabled}
          >
            <RefreshCw size={15} />
          </button>
        </Show>
      </div>
      <Show when={error()}>{(msg) => <p class="mt-1 text-[11px] text-error">{msg()}</p>}</Show>
    </div>
  );
}
