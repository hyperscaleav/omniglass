import { Show, createSignal } from "solid-js";
import { useNavigate, useSearchParams } from "@solidjs/router";
import { useLogin } from "../lib/auth";

// Login is a bearer-token paste form (the backend is bearer-only this slice).
// On success it stores + validates the token and lands at ?next= (an in-app
// path) or Home. It is mounted outside the App shell, so it has a bare layout.
export default function Login() {
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const login = useLogin();

  const [token, setTokenInput] = createSignal("");
  const [error, setError] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);

  const next = (): string => {
    const raw = typeof params.next === "string" ? decodeURIComponent(params.next) : "/";
    return raw.startsWith("/") && !raw.startsWith("//") ? raw.replace(/^\/web/, "") || "/" : "/";
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
    <div style={{ "min-height": "100vh", background: "var(--ground)", display: "flex", "align-items": "center", "justify-content": "center", padding: "24px" }}>
      <div class="card" style={{ width: "100%", "max-width": "380px", padding: "24px" }}>
        <div style={{ "margin-bottom": "18px" }}>
          <h1 class="mono" style={{ "font-size": "20px", "font-weight": 700 }}>
            <span>omni</span><span style={{ color: "var(--primary)" }}>glass</span>
          </h1>
          <p style={{ "font-size": "13px", color: "var(--text-dim)", "margin-top": "4px" }}>Sign in to the operator console.</p>
        </div>
        <form onSubmit={onSubmit} style={{ display: "flex", "flex-direction": "column", gap: "12px" }}>
          <div>
            <label class="eyebrow" for="login-token" style={{ display: "block", "margin-bottom": "6px" }}>Bearer token</label>
            <input
              id="login-token"
              type="password"
              class="input"
              style={{ width: "100%" }}
              placeholder="ogp_…"
              value={token()}
              onInput={(e) => setTokenInput(e.currentTarget.value)}
              disabled={busy()}
              autofocus
              required
            />
          </div>
          <Show when={error()}>
            <div role="alert" class="badge" style={{ color: "var(--high)", "border-color": "color-mix(in oklch, var(--high) 45%, transparent)", background: "color-mix(in oklch, var(--high) 13%, transparent)", padding: "8px 10px" }}>
              {error()}
            </div>
          </Show>
          <button type="submit" class="btn btn-primary" disabled={busy() || !token()} style={{ width: "100%" }}>
            {busy() ? "Checking…" : "Sign in"}
          </button>
        </form>
        <p style={{ "font-size": "11.5px", color: "var(--text-faint)", "margin-top": "16px", "line-height": 1.5 }}>
          No token? Run <span class="mono" style={{ color: "var(--text-dim)" }}>omniglass bootstrap &lt;username&gt;</span> on the server to mint the first owner's token.
        </p>
      </div>
    </div>
  );
}
