import { createSignal, Show } from "solid-js";
import PasswordField from "./PasswordField";
import { passwordError, isPasswordPolicyMessage } from "../lib/validate";
import { useMe, useChangePassword, useLogout } from "../lib/auth";

// ForceChangePassword is the full-screen gate shown when an administrator has reset
// the caller's password (me.human.must_change_password). The server refuses every
// other route until the password is changed, so the console mirrors that: this
// replaces the whole app shell until the change succeeds, which clears the flag (via
// the /auth/me invalidation in useChangePassword) and releases the gate. Signing out
// is the only other way off this screen.
export default function ForceChangePassword() {
  const me = useMe();
  const changePassword = useChangePassword();
  const logout = useLogout();
  const username = () => me.data?.human?.username;

  const [current, setCurrent] = createSignal("");
  const [next, setNext] = createSignal("");
  const [confirm, setConfirm] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [error, setError] = createSignal<string | null>(null);
  const [fieldError, setFieldError] = createSignal<string | null>(null);

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    if (next() !== confirm()) {
      setError("The new passwords do not match.");
      return;
    }
    setBusy(true);
    setError(null);
    setFieldError(null);
    const r = await changePassword(current(), next());
    if (!r.ok) {
      if (isPasswordPolicyMessage(r.message)) setFieldError(r.message);
      else setError(r.message);
      setBusy(false);
      return;
    }
    // On success the gate releases itself: the ME_KEY invalidation refreshes
    // must_change_password to false and AuthGuard renders the app.
  }

  const canSubmit = () =>
    !busy() && !!current() && !!next() && next() === confirm() && !passwordError(next(), username());

  return (
    <div class="flex min-h-screen items-center justify-center bg-base-100 p-4">
      <form onSubmit={submit} class="card w-full max-w-md border border-base-300 bg-base-200 shadow-lg">
        <div class="card-body gap-3">
          <h1 class="card-title text-base">Set a new password</h1>
          <p class="text-sm text-base-content/60">
            An administrator reset your password, so you need to choose a new one before you
            can continue. Your account is on hold until you do.
          </p>
          <div>
            <label class="eyebrow mb-1.5 block" for="fc-current">Current password</label>
            <input
              id="fc-current"
              type="password"
              autocomplete="current-password"
              class="input input-bordered w-full font-data"
              value={current()}
              onInput={(e) => setCurrent(e.currentTarget.value)}
              disabled={busy()}
              placeholder="the password you were given"
            />
            <p class="mt-1 text-[11px] text-base-content/40">The temporary password your administrator gave you.</p>
          </div>
          <div>
            <label class="eyebrow mb-1.5 block" for="fc-new">New password</label>
            <PasswordField
              id="fc-new"
              value={next()}
              onInput={(v) => { setNext(v); setFieldError(null); }}
              username={username()}
              disabled={busy()}
              serverError={fieldError()}
              required
              generate
            />
          </div>
          <div>
            <label class="eyebrow mb-1.5 block" for="fc-confirm">Confirm new password</label>
            <input
              id="fc-confirm"
              type="password"
              autocomplete="new-password"
              class="input input-bordered w-full font-data"
              value={confirm()}
              onInput={(e) => setConfirm(e.currentTarget.value)}
              disabled={busy()}
            />
          </div>
          <Show when={error()}>
            <div class="alert alert-error alert-soft py-2 text-sm">{error()}</div>
          </Show>
          <div class="card-actions items-center justify-between">
            <button type="button" class="btn btn-quiet btn-sm" onClick={() => logout()}>
              Sign out
            </button>
            <button type="submit" class="btn btn-action btn-sm" disabled={!canSubmit()}>
              {busy() ? "Saving…" : "Set password"}
            </button>
          </div>
        </div>
      </form>
    </div>
  );
}
