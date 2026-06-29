import { Show, createSignal } from "solid-js";
import { useNavigate, useSearchParams } from "@solidjs/router";
import { useLogin } from "../lib/auth";

// Login is a bearer-token paste form (the backend is bearer-only this slice).
// On success it stores + validates the token and lands at ?next= (an in-app
// path) or Home. Mounted outside the App shell, so it has a bare layout.
export default function Login() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const login = useLogin();

  const [token, setTokenInput] = createSignal("");
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
      const result = await login(token());
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
            <h1 class="og-wordmark font-data text-xl font-bold">omni<span class="text-primary">glass</span></h1>
            <p class="mt-1 text-sm text-base-content/60">Sign in to the operator console.</p>
          </div>
          <form onSubmit={onSubmit} class="flex flex-col gap-3">
            <div>
              <label class="eyebrow mb-1.5 block" for="login-token">Bearer token</label>
              <input
                id="login-token"
                type="password"
                class="input input-bordered w-full"
                placeholder="ogp_…"
                value={token()}
                onInput={(e) => setTokenInput(e.currentTarget.value)}
                disabled={busy()}
                autofocus
                required
              />
            </div>
            <Show when={error()}>
              <div role="alert" class="alert alert-error alert-soft text-sm"><span>{error()}</span></div>
            </Show>
            <button type="submit" class="btn btn-primary w-full" disabled={busy() || !token()}>
              <Show when={busy()}><span class="loading loading-spinner loading-xs" /></Show>
              {busy() ? "Checking…" : "Sign in"}
            </button>
          </form>
          <p class="text-[11px] leading-relaxed text-base-content/40">
            No token? Run <span class="font-data text-base-content/60">omniglass bootstrap &lt;username&gt;</span> on the server to mint the first owner's token.
          </p>
        </div>
      </div>
    </div>
  );
}
