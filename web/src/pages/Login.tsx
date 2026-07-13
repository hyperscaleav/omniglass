import { Show, createSignal, createResource } from "solid-js";
import { useNavigate, useSearchParams } from "@solidjs/router";
import { useLogin, useTokenLogin } from "../lib/auth";
import { api } from "../api/client";
import { BrandMark, Wordmark } from "../components/Brand";
import Button from "../components/Button";

// Login signs in with a username + password (the default, session-cookie path) or,
// behind a toggle, a pasted bearer token. On success it lands at ?next= (a safe
// in-app path) or Home. Mounted outside the App shell, so it has a bare layout.
export default function Login() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const login = useLogin();
  const tokenLogin = useTokenLogin();

  const [mode, setMode] = createSignal<"password" | "token">("password");
  const [username, setUsername] = createSignal("");
  const [password, setPassword] = createSignal("");
  const [token, setTokenInput] = createSignal("");
  const [error, setError] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);

  // Whether an owner exists yet: the bootstrap hint shows only before the first
  // owner is created. Default to true (and undefined while loading) so the hint
  // never flashes for an already-bootstrapped system.
  const [bootstrapped] = createResource(async () => {
    const { data } = await api.GET("/auth/status");
    return data?.bootstrapped ?? true;
  });

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

  const canSubmit = () => (mode() === "password" ? !!username() && !!password() : !!token());

  async function onSubmit(e: SubmitEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      const result = mode() === "password" ? await login(username().trim(), password()) : await tokenLogin(token());
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
            <Show
              when={mode() === "password"}
              fallback={
                <div>
                  <label class="eyebrow mb-1.5 block" for="login-token">Bearer token</label>
                  <input
                    id="login-token"
                    type="password"
                    autocomplete="off"
                    class="input input-bordered w-full"
                    placeholder="ogp_…"
                    value={token()}
                    onInput={(e) => setTokenInput(e.currentTarget.value)}
                    disabled={busy()}
                    required
                  />
                </div>
              }
            >
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
            </Show>
            <Show when={error()}>
              <div role="alert" class="alert alert-error alert-soft text-sm"><span>{error()}</span></div>
            </Show>
            <Button type="submit" intent="action" size="md" class="w-full" loading={busy()} disabled={!canSubmit()}>
              {busy() ? "Signing in…" : "Sign in"}
            </Button>
          </form>
          <button
            type="button"
            class="link self-start text-xs text-base-content/50"
            onClick={() => {
              setMode((m) => (m === "password" ? "token" : "password"));
              setError(null);
            }}
          >
            {mode() === "password" ? "Use a bearer token instead" : "Use a username and password"}
          </button>
          <Show when={bootstrapped() === false}>
            <p class="text-[11px] leading-relaxed text-base-content/40">
              No account yet. Run <span class="font-data text-base-content/60">omniglass bootstrap &lt;username&gt; --password &lt;password&gt;</span> on the server to create the first owner.
            </p>
          </Show>
        </div>
      </div>
    </div>
  );
}
