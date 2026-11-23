import { Show, createSignal, createResource } from "solid-js";
import { useSearchParams } from "@solidjs/router";
import { api } from "../api/client";
import Button from "./Button";
import IdentityLinksList from "./IdentityLinksList";
import { useMyIdentityLinks, useUnlinkSelf, startSelfLink, type IdentityLink } from "../lib/identityLinks";

// SelfIdentityLinks is the profile's single sign-on card (#2362): the
// identities at the identity provider you sign in through, linking your own
// (confirmed by your current password, then a sign-in at the provider bound
// to this session) and unlinking one. It is how an owner gains single sign-on,
// since no one else may link an owner. Rendered when single sign-on is
// configured or you already hold a link.
export default function SelfIdentityLinks() {
  const [params] = useSearchParams();
  const links = useMyIdentityLinks();
  const unlinkSelf = useUnlinkSelf();
  const [status] = createResource(async () => {
    const { data } = await api.GET("/auth/status");
    return (data?.sso_providers ?? []) as string[];
  });
  const [password, setPassword] = createSignal("");
  const [pending, setPending] = createSignal<"link" | IdentityLink | null>(null);
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);
  const linked = () => params.sso_linked === "1";
  const callbackError = () => (typeof params.sso_error === "string" ? params.sso_error : null);
  const visible = () => (status() ?? []).length > 0 || (links.data ?? []).length > 0;

  function ask(what: "link" | IdentityLink) {
    setPending(what);
    setPassword("");
    setErr(null);
  }
  async function confirm() {
    const what = pending();
    if (!what) return;
    setBusy(true);
    setErr(null);
    if (what === "link") {
      const r = await startSelfLink(password());
      if (r.ok) {
        window.location.assign(r.url);
        return;
      }
      setErr(r.message);
    } else {
      const r = await unlinkSelf(what.id, password());
      if (r.ok) setPending(null);
      else setErr(r.message);
    }
    setBusy(false);
  }

  return (
    <Show when={visible()}>
      <div class="card border border-base-300 bg-base-200" data-testid="profile-sso">
        <div class="card-body gap-3">
          <div class="flex items-center justify-between gap-2">
            <h2 class="card-title text-base">Single sign-on</h2>
            <Show when={(status() ?? []).length > 0 && pending() === null}>
              <Button intent="action" size="xs" onClick={() => ask("link")}>Link your identity</Button>
            </Show>
          </div>
          <p class="text-xs text-base-content/50">
            The identities at your organization's identity provider you sign in through. Each is the provider's issuer and
            your subject there, <span class="font-data text-base-content/70">iss|sub</span>. Linking asks for your password
            because a link outlives this session, then sends you to the provider to sign in as the identity to link.
          </p>
          <Show when={linked()}>
            <div role="alert" class="alert alert-success alert-soft text-sm"><span>Linked. You can now sign in with single sign-on.</span></div>
          </Show>
          <Show when={callbackError()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{callbackError()}</span></div>
          </Show>
          <IdentityLinksList links={links.data ?? []} onUnlink={(l) => ask(l)} emptyLabel="Not linked: you sign in with your password." />
          <Show when={pending()}>
            <div class="flex flex-col gap-2 rounded-box border border-base-300 bg-base-100 p-3">
              <label class="eyebrow block" for="sso-confirm-pw">
                {pending() === "link" ? "Confirm your password to link" : "Confirm your password to unlink"}
              </label>
              <input
                id="sso-confirm-pw"
                type="password"
                autocomplete="current-password"
                class="input input-bordered input-sm w-full"
                value={password()}
                onInput={(e) => { setPassword(e.currentTarget.value); setErr(null); }}
              />
              <Show when={err()}>
                <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
              </Show>
              <div class="flex justify-end gap-2">
                <Button size="sm" onClick={() => setPending(null)} disabled={busy()}>Cancel</Button>
                <Button size="sm" intent={pending() === "link" ? "action" : "danger"} loading={busy()} disabled={!password()} onClick={confirm}>
                  {pending() === "link" ? "Continue to the provider" : "Unlink"}
                </Button>
              </div>
            </div>
          </Show>
        </div>
      </div>
    </Show>
  );
}
