import { Show, createSignal } from "solid-js";
import { useNavigate, useSearchParams } from "@solidjs/router";
import { useLogin } from "../lib/auth";
import { BrandMark, Wordmark } from "../components/Brand";

// Login is the username + password form. On success the server sets the session
// cookie and the form lands at ?next= (an in-app path) or Home. Mounted outside
// the App shell, so it has a bare layout.
export default function Login() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const login = useLogin();

  const [username, setUsername] = createSignal("");
  const [password, setPassword] = createSignal("");
  const [error, setError] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);

  // Resolve ?next= to a safe in-app path. Parsing against the current origin and
  // refusing cross-origin results blocks an open redirect; the /web base is
  // stripped so the router does not double-count it.
  const next = (): string => {
    const raw = typeof params.next === "string" ? params.next : "/";
    try {
      const u = new URL(decodeURIComponent(raw), window.location.origin);
      if (u.origin !== window.location.origin) return "/";
      const p = u.pathname.replace(/^\/web/, "") || "/";
      return p.startsWith("/") ? p : "/";
    } catch {
      return "/";
    }
  };

  async function onSubmit(e: SubmitEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      const result = await login(username().trim(), password());
      if (!result.ok) {
        setError(result.message);
        return;
      }
      navigate(next(), { replace: true });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div class="flex min-h-screen items-center justify-center bg-base-100 px-6">
      <div class="card w-full max-w-sm border border-base-300 bg-base-200">
        <div class="card-body gap-4">
          <div>
            <div class="flex items-center gap-2.5">
              <BrandMark size={28} />
              <Wordmark class="text-xl" />
            </div>
            <p class="mt-1.5 text-sm text-base-content/60">Sign in to the operator console.</p>
          </div>
          <form onSubmit={onSubmit} class="flex flex-col gap-3">
            <div>
              <label class="eyebrow mb-1.5 block" for="login-username">Username</label>
              <input
                id="login-username"
                type="text"
                autocomplete="username"
                class="input input-bordered w-full"
                placeholder="ops"
                value={username()}
                onInput={(e) => setUsername(e.currentTarget.value)}
                disabled={busy()}
                autofocus
                required
              />
            </div>
            <div>
              <label class="eyebrow mb-1.5 block" for="login-password">Password</label>
              <input
                id="login-password"
                type="password"
                autocomplete="current-password"
                class="input input-bordered w-full"
                value={password()}
                onInput={(e) => setPassword(e.currentTarget.value)}
                disabled={busy()}
                required
              />
            </div>
            <Show when={error()}>
              <div role="alert" class="alert alert-error alert-soft text-sm"><span>{error()}</span></div>
            </Show>
            <button type="submit" class="btn btn-primary w-full" disabled={busy() || !username() || !password()}>
              <Show when={busy()}><span class="loading loading-spinner loading-xs" /></Show>
              {busy() ? "Signing in…" : "Sign in"}
            </button>
          </form>
          <p class="text-[11px] leading-relaxed text-base-content/40">
            No account? Run <span class="font-data text-base-content/60">omniglass bootstrap &lt;username&gt; --password &lt;password&gt;</span> on the server to create the first owner.
          </p>
        </div>
      </div>
    </div>
  );
}
