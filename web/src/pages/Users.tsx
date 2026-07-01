import { For, Show, createMemo, createSignal } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Page from "../components/Page";
import { type Principal, type Grant, PRINCIPALS_KEY, listPrincipals, createPrincipal, principalName } from "../lib/principals";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { Plus } from "../components/icons";

// Users: the admin principal directory. A read grid of every principal (human or
// service account) with its role grants, a detail panel for the selected one, and
// a create form for a new human (gated by principal:create). It is self-teaching:
// the detail panel shows the grant model (role x scope) the platform enforces.
const kindBadge = (kind: string) => `badge badge-soft badge-sm capitalize ${kind === "service" ? "badge-info" : "badge-primary"}`;

function grantLabel(g: Grant): string {
  return `${g.role} @ ${g.scope_kind}${g.scope_id ? `:${g.scope_id}` : ""}`;
}

export default function Users() {
  const qc = useQueryClient();
  const me = useMe();
  const principals = useQuery(() => ({ queryKey: PRINCIPALS_KEY, queryFn: () => listPrincipals() }));

  const [selectedId, setSelectedId] = createSignal<string | null>(null);
  const selected = createMemo(() => principals.data?.find((p) => p.id === selectedId()) ?? null);
  const [createOpen, setCreateOpen] = createSignal(false);

  const initials = (p: Principal) => principalName(p).slice(0, 2).toUpperCase();

  return (
    <Page title="Users">
      <div class="mb-4 flex items-center gap-3">
        <p class="text-sm text-base-content/60">Every principal that can authenticate: operators and service accounts, with the roles granted to each.</p>
        <span class="flex-1" />
        <Show when={can(me.data, "principal", "create")}>
          <button class="btn btn-primary btn-sm gap-1.5" onClick={() => setCreateOpen(true)}><Plus size={14} /> New user</button>
        </Show>
      </div>

      <Show when={principals.error}>
        <div role="alert" class="alert alert-error alert-soft mb-4 text-sm"><span>{describeError(principals.error)}</span></div>
      </Show>

      <div class="grid gap-4 lg:grid-cols-[1.4fr_1fr]">
        {/* Directory grid */}
        <div class="overflow-hidden rounded-box border border-base-300">
          <table class="table table-sm">
            <thead>
              <tr>
                <th>Name</th>
                <th>Kind</th>
                <th>Grants</th>
              </tr>
            </thead>
            <tbody>
              <For each={principals.data ?? []} fallback={<tr><td colspan="3" class="text-center text-base-content/40">{principals.isLoading ? "Loading…" : "No principals"}</td></tr>}>
                {(p) => (
                  <tr
                    class="cursor-pointer hover:bg-base-content/5"
                    classList={{ "bg-primary/10": p.id === selectedId() }}
                    onClick={() => setSelectedId(p.id)}
                  >
                    <td>
                      <div class="flex items-center gap-2.5">
                        <div class="avatar avatar-placeholder">
                          <div class="w-7 rounded-full bg-linear-to-br from-primary to-info text-primary-content">
                            <span class="font-data text-[10px] font-bold uppercase">{initials(p)}</span>
                          </div>
                        </div>
                        <div class="min-w-0 leading-tight">
                          <div class="truncate text-sm font-medium">{principalName(p)}</div>
                          <Show when={p.human}><div class="truncate font-data text-[11px] text-base-content/40">{p.human!.username}</div></Show>
                        </div>
                      </div>
                    </td>
                    <td><span class={kindBadge(p.kind)}>{p.kind}</span></td>
                    <td class="tnum text-base-content/60">{p.grants.length}</td>
                  </tr>
                )}
              </For>
            </tbody>
          </table>
        </div>

        {/* Detail panel */}
        <div class="card h-fit border border-base-300 bg-base-200">
          <div class="card-body gap-3">
            <Show when={selected()} fallback={<p class="py-8 text-center text-sm text-base-content/40">Select a user to see its profile and grants.</p>}>
              {(p) => (
                <>
                  <div class="flex items-center gap-3">
                    <div class="avatar avatar-placeholder">
                      <div class="w-12 rounded-full bg-linear-to-br from-primary to-info text-primary-content">
                        <span class="font-data text-sm font-bold uppercase">{initials(p())}</span>
                      </div>
                    </div>
                    <div class="min-w-0">
                      <div class="truncate text-base font-semibold">{principalName(p())}</div>
                      <span class={kindBadge(p().kind)}>{p().kind}</span>
                    </div>
                  </div>
                  <div class="grid grid-cols-2 gap-3 text-sm">
                    <Show when={p().human}>
                      <Fact label="Username" value={<span class="font-data">{p().human!.username}</span>} />
                      <Fact label="Email" value={p().human!.email || <span class="text-base-content/40">not set</span>} />
                    </Show>
                    <Show when={p().service}>
                      <Fact label="Label" value={<span class="font-data">{p().service!.label}</span>} />
                    </Show>
                  </div>
                  <div>
                    <div class="eyebrow mb-1.5">Role grants</div>
                    <div class="flex flex-wrap gap-1.5">
                      <For each={p().grants} fallback={<span class="text-xs text-base-content/40">No grants yet. This principal can sign in but has no permissions.</span>}>
                        {(g) => <span class="badge badge-soft badge-primary font-data text-[11px]">{grantLabel(g)}</span>}
                      </For>
                    </div>
                  </div>
                </>
              )}
            </Show>
          </div>
        </div>
      </div>

      <Show when={createOpen()}>
        <CreateUserModal
          close={() => setCreateOpen(false)}
          onCreated={async (p) => {
            await qc.invalidateQueries({ queryKey: PRINCIPALS_KEY });
            setSelectedId(p.id);
            setCreateOpen(false);
          }}
        />
      </Show>
    </Page>
  );
}

function Fact(props: { label: string; value: unknown }) {
  return (
    <div>
      <div class="eyebrow">{props.label}</div>
      <div>{props.value as never}</div>
    </div>
  );
}

// CreateUserModal is the new-human form: username (required), display name, email,
// and an optional initial password (min 8) the user changes after signing in.
function CreateUserModal(props: { close: () => void; onCreated: (p: Principal) => void | Promise<void> }) {
  const [username, setUsername] = createSignal("");
  const [displayName, setDisplayName] = createSignal("");
  const [email, setEmail] = createSignal("");
  const [password, setPassword] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      const created = await createPrincipal({
        username: username().trim(),
        display_name: displayName().trim() || undefined,
        email: email().trim() || undefined,
        password: password() || undefined,
      });
      await props.onCreated(created);
    } catch (er) {
      setErr(describeError(er));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div class="modal modal-open">
      <div class="modal-box">
        <h3 class="text-base font-semibold">New user</h3>
        <p class="mb-3 mt-1 text-xs text-base-content/50">Creates a human principal. Assign roles afterwards; a user with no grants can sign in but has no permissions.</p>
        <form class="flex flex-col gap-3" onSubmit={submit}>
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div>
            <label class="eyebrow mb-1.5 block" for="new-username">Username</label>
            <input id="new-username" autocomplete="off" class="input input-bordered w-full font-data" value={username()} placeholder="jordan" onInput={(e) => setUsername(e.currentTarget.value)} disabled={busy()} required />
          </div>
          <div>
            <label class="eyebrow mb-1.5 block" for="new-display">Display name</label>
            <input id="new-display" autocomplete="off" class="input input-bordered w-full" value={displayName()} placeholder="Jordan Rivera" onInput={(e) => setDisplayName(e.currentTarget.value)} disabled={busy()} />
          </div>
          <div>
            <label class="eyebrow mb-1.5 block" for="new-email">Email</label>
            <input id="new-email" type="email" autocomplete="off" class="input input-bordered w-full" value={email()} onInput={(e) => setEmail(e.currentTarget.value)} disabled={busy()} />
          </div>
          <div>
            <label class="eyebrow mb-1.5 block" for="new-password">Initial password</label>
            <input id="new-password" type="password" autocomplete="new-password" minLength={8} class="input input-bordered w-full" value={password()} onInput={(e) => setPassword(e.currentTarget.value)} disabled={busy()} />
            <p class="mt-1 text-[11px] text-base-content/40">Optional, at least 8 characters. The user changes it after signing in.</p>
          </div>
          <div class="mt-1 flex justify-end gap-2">
            <button type="button" class="btn btn-ghost btn-sm" onClick={props.close} disabled={busy()}>Cancel</button>
            <button type="submit" class="btn btn-primary btn-sm" disabled={busy() || !username().trim()}>
              <Show when={busy()}><span class="loading loading-spinner loading-xs" /></Show>
              Create user
            </button>
          </div>
        </form>
      </div>
      <button class="modal-backdrop" onClick={props.close} aria-label="Close" />
    </div>
  );
}
