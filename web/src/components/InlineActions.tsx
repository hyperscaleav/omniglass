import { Show, createSignal, type JSX } from "solid-js";
import { Eye, EyeOff, Copy, Check, RefreshCw } from "./icons";

// The console's one inline-action button family: the reveal / copy / generate
// buttons that sit in a daisyUI `join` after a value or input (PasswordField,
// SecretFields, and any KVRow actions slot). Each is a square bordered join-item
// so it shares the field's rounded right edge and the collapsed-hover behavior;
// the copy button owns the transient "copied" check, so that timeout logic lives
// in one place instead of being reimplemented per field.
const BTN = "btn btn-bordered join-item btn-square";

// RevealButton toggles a masked value's visibility (the eye). Presentational:
// the parent owns whether the value is currently revealed and what revealing
// means (a secret decrypts through the audited reveal endpoint).
export function RevealButton(props: {
  revealed: boolean;
  onToggle: () => void;
  label: string;
  disabled?: boolean;
}): JSX.Element {
  return (
    <button
      type="button"
      class={BTN}
      aria-label={props.revealed ? `Hide ${props.label}` : `Reveal ${props.label}`}
      title={props.revealed ? "Hide" : "Reveal"}
      onClick={() => props.onToggle()}
      disabled={props.disabled}
    >
      <Show when={props.revealed} fallback={<Eye size={15} />}><EyeOff size={15} /></Show>
    </button>
  );
}

// CopyButton copies a value and flashes a check for 1.5s on success. `onCopy`
// does the actual copy (and, for a secret, the audited decrypt) and returns
// true when the clipboard write succeeded; the button owns only the check state,
// so the parent keeps its own error handling.
export function CopyButton(props: {
  onCopy: () => boolean | Promise<boolean>;
  label: string;
  disabled?: boolean;
}): JSX.Element {
  const [copied, setCopied] = createSignal(false);
  const doCopy = async () => {
    if (!(await props.onCopy())) return;
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };
  return (
    <button
      type="button"
      class={BTN}
      aria-label={`Copy ${props.label}`}
      title="Copy"
      onClick={doCopy}
      disabled={props.disabled}
    >
      <Show when={copied()} fallback={<Copy size={15} />}><Check size={15} /></Show>
    </button>
  );
}

// GenerateButton fills a fresh value (a crypto-strong password). The `label` is
// the action description (used for the accessible name and the tooltip).
export function GenerateButton(props: {
  onGenerate: () => void;
  label: string;
  disabled?: boolean;
}): JSX.Element {
  return (
    <button
      type="button"
      class={BTN}
      aria-label={props.label}
      title={props.label}
      onClick={() => props.onGenerate()}
      disabled={props.disabled}
    >
      <RefreshCw size={15} />
    </button>
  );
}
