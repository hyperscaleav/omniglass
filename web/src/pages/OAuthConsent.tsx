import { Show, For, createSignal, createResource } from "solid-js";
import { useSearchParams } from "@solidjs/router";
import Button from "../components/Button";
import { Ban, Check } from "../components/icons";
import { consentRequest, fetchOAuthClient, answerConsent, redirectHost } from "../lib/oauth";

// OAuthConsent is where a person lets an MCP client act for them (#2048). The
// client registered itself and sent the person here through /oauth/authorize;
// the page names the client and where the answer goes, and lists the roles the
// person may hand it. Approving sends a one-time code back to the client, which
// exchanges it for an agent token: the token acts as the client's agent, holds
// only what the chosen roles AND the person hold, and never reads a secret's
// plaintext. Denying sends access_denied back instead.
export default function OAuthConsent() {
  const [params] = useSearchParams();
  const req = consentRequest(params);
  const [client] = createResource(() => req?.client_id, fetchOAuthClient);

  const [chosen, setChosen] = createSignal<Set<string>>(new Set());
  function toggle(role: string) {
    const next = new Set(chosen());
    if (next.has(role)) next.delete(role);
    else next.add(role);
    setChosen(next);
  }

  const [busy, setBusy] = createSignal<"approve" | "deny" | null>(null);
  const [err, setErr] = createSignal<string | null>(null);
  async function answer(approved: boolean) {
    if (!req) return;
    setBusy(approved ? "approve" : "deny");
    setErr(null);
    const r = await answerConsent(req, approved, [...chosen()].sort());
    if (r.ok) {
      window.location.assign(r.redirect_to);
      return;
    }
    setErr(r.message);
    setBusy(null);
  }

  const invalid = (text: string) => (
    <div role="alert" class="alert alert-soft alert-error text-sm">
      <span>{text}</span>
    </div>
  );

  return (
    <section class="og-stack flex flex-col">
      <div class="mx-auto w-full max-w-xl">
        <Show when={req} fallback={invalid("This sign-in request is incomplete. Start the sign-in again from your MCP client.")}>
          <Show when={!client.loading} fallback={<span class="loading loading-spinner" aria-label="Loading" />}>
            <Show when={!client.error} fallback={invalid("Could not read the client. Try again.")}>
              <Show when={client()} fallback={invalid("No client is registered under this sign-in request. Start the sign-in again from your MCP client.")}>
                {(c) => (
                  <div class="card border border-base-300 bg-base-200">
                    <div class="card-body gap-4">
                      <h2 class="card-title text-base">Connect {c().client_name}</h2>
                      <p class="text-sm">
                        <span class="font-semibold">{c().client_name}</span> is asking to act for you over MCP. Its answer goes back to{" "}
                        <span class="font-data">{redirectHost(req!.redirect_uri)}</span>.
                      </p>

                      <fieldset class="flex flex-col gap-2">
                        <legend class="mb-1 text-sm font-semibold">Roles it may act under</legend>
                        <Show when={c().roles.length > 0} fallback={<p class="text-sm text-base-content/70">You hold no role you could hand a client.</p>}>
                          <For each={c().roles}>
                            {(role) => (
                              <label class="flex items-center gap-2 text-sm">
                                <input type="checkbox" class="checkbox checkbox-sm" checked={chosen().has(role)} onChange={() => toggle(role)} />
                                <span class="font-data">{role}</span>
                              </label>
                            )}
                          </For>
                        </Show>
                      </fieldset>

                      <div class="text-sm text-base-content/70">
                        <p class="mb-1 font-semibold text-base-content">What the client gets</p>
                        <ul class="list-disc space-y-1 pl-5">
                          <li>An agent token for MCP only: it may do what the roles you choose allow and you are allowed, checked again on every call.</li>
                          <li>Its actions are audited as {c().client_name}, acting for you.</li>
                          <li>It can use a secret where you may, but never read one: revealing, copying and enrollment tokens stay yours.</li>
                          <li>It lasts 30 days. Revoke it from your profile's API tokens and the client's next call is refused.</li>
                        </ul>
                      </div>

                      <Show when={err()}>{(m) => invalid(m())}</Show>

                      <div class="card-actions justify-end">
                        <Button intent="quiet" icon={Ban} loading={busy() === "deny"} disabled={busy() !== null} onClick={() => answer(false)}>
                          Deny
                        </Button>
                        <Button
                          intent="action"
                          icon={Check}
                          loading={busy() === "approve"}
                          disabled={busy() !== null || chosen().size === 0}
                          onClick={() => answer(true)}
                        >
                          Approve
                        </Button>
                      </div>
                    </div>
                  </div>
                )}
              </Show>
            </Show>
          </Show>
        </Show>
      </div>
    </section>
  );
}
