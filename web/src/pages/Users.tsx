import { For, Show, createSignal } from "solid-js";
import { useSearchParams } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import { useFormActions } from "../lib/formactions";
import PasswordField from "../components/PasswordField";
import { type Principal, PRINCIPALS_KEY, listPrincipals, createPrincipal, openPrincipalInEdit, principalName, kindBadge } from "../lib/principals";
import { identityColumn } from "../components/IdentityCell";
import { createIdentity, type Labelled } from "../lib/entities";
import UserAvatar from "../components/UserAvatar";
import { identityRegistry } from "../lib/identityBlades";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { handleError, emailError, passwordError, isPasswordPolicyMessage } from "../lib/validate";
import type { FilterKey } from "../lib/predicate";
import { Plus } from "../components/icons";

// Users: the admin principal directory, a config over the shared FlatList. A row per
// principal (human or service account) opens its detail as a blade (rooted here on
// user, drilling into the groups it belongs to); "New user" opens the create Drawer
// (gated by principal:create). It is self-teaching: the detail shows the grant model
// (role x scope) the platform enforces. Every gate is a UI hint; the server is
// authority. The blade bodies live in UserDetail / GroupDetail (the cross-entity
// registry), so a user's group drills to a group blade over it.

const filterKeys: FilterKey<Principal>[] = [
  { key: "name", type: "string", hint: "substring", get: (p) => principalName(p) },
  { key: "kind", type: "string", hint: "exact", get: (p) => p.kind, values: (rows) => [...new Set(rows.map((r) => r.kind))].sort() },
  { key: "username", type: "string", hint: "substring", get: (p) => p.human?.username ?? "" },
];

// A principal wears the two identities the console labels everything by, under its
// own field names: the username is the segment (what the API, the CLI, and the sign-in
// prompt address a human by) and the display name is the label. A service account has
// only its label, so that stands as the segment and nothing sits beneath it.
const principalIdentity = (p: Principal): Labelled => ({
  name: p.human?.username ?? p.service?.label ?? p.kind,
  display_name: p.human?.display_name,
});

const identity = identityColumn<Labelled>();

// The directory columns. Name carries the avatar plus the shared identity cell;
// Kind the human/service badge (with an inactive marker); Grants the grant count;
// Groups the badges of the groups the principal belongs to.
const columns: FlatColumn<Principal>[] = [
  {
    ...identity,
    // The avatar is a principal's own identity cue and stays; the two lines beside
    // it are the shared cell, read through the adapter above.
    sortVal: (p: Principal) => identity.sortVal(principalIdentity(p)),
    cell: (p: Principal) => (
      <div class="flex items-center gap-2.5">
        <UserAvatar principal={p} size="w-7" textClass="text-[10px]" />
        {identity.cell(principalIdentity(p))}
      </div>
    ),
  },
  {
    key: "kind", label: "Kind", width: "180px", sortVal: (p) => p.kind,
    cell: (p) => (
      <>
        <span class={kindBadge(p.kind)}>{p.kind}</span>
        <Show when={p.archived_at} fallback={<Show when={!p.active}><span class="badge badge-soft badge-warning badge-sm ml-1">inactive</span></Show>}>
          <span class="badge badge-soft badge-error badge-sm ml-1">archived</span>
        </Show>
      </>
    ),
  },
  {
    key: "grants", label: "Grants", width: "100px", sortVal: (p) => p.grants.length,
    cell: (p) => <span class="tnum text-base-content/60">{p.grants.length}</span>,
  },
  {
    key: "groups", label: "Groups",
    cell: (p) => (
      <Show when={p.groups?.length} fallback={<span class="text-base-content/30">—</span>}>
        <span class="inline-flex flex-wrap gap-1">
          <For each={p.groups}>{(g) => <span class="badge badge-ghost badge-sm">{g.name}</span>}</For>
        </span>
      </Show>
    ),
  },
];

export default function Users() {
  const me = useMe();
  const [params] = useSearchParams();
  // Archived (soft-deleted) principals are hidden by default; the toggle includes
  // them (a distinct query key) so an admin can re-find one to restore or purge.
  const [showArchived, setShowArchived] = createSignal(false);
  const principals = useQuery(() => ({ queryKey: [...PRINCIPALS_KEY, showArchived()], queryFn: () => listPrincipals(undefined, showArchived()) }));
  // ?u=<id> deep-links to a user (e.g. the cross-over from a group's member).
  const openId = () => (Array.isArray(params.u) ? params.u[0] : params.u) || undefined;

  return (
    <FlatList<Principal>
      config={{
        entity: { name: "user", plural: "users" },
        rows: () => principals.data ?? [],
        loading: () => principals.isLoading,
        error: () => principals.error,
        filterKeys,
        filterPlaceholder: "filter by name, kind, username",
        columns,
        empty: "No users yet.",
        rowId: (p) => p.id,
        openId,
        blades: { registry: identityRegistry, rootKind: "user" },
        railExtra: () => (
          <label class="flex cursor-pointer items-center gap-2 text-xs text-base-content/60">
            <input type="checkbox" class="toggle toggle-xs" checked={showArchived()} onChange={(e) => setShowArchived(e.currentTarget.checked)} />
            Show archived
          </label>
        ),
        create: {
          label: "New user",
          can: () => can(me.data, "principal", "create"),
          body: (ctx) => <CreateUserForm close={ctx.close} onCreated={ctx.select} />,
        },
      }}
    />
  );
}

// CreateUserForm is the new-human form the create Drawer hosts: display name and
// username (required), email, and an optional initial password (min 8) the user
// changes after signing in. On success it invalidates the directory and hands the
// created principal to onCreated, which opens its detail blade (closing this Drawer).
//
// The two identity fields are the shared coupling: the display name leads and the
// username derives from it, so an operator types "Jordan Rivera" without meeting the
// handle character class, and the moment they edit the username it is theirs.
function CreateUserForm(props: { close: () => void; onCreated: (p: Principal) => void }) {
  const qc = useQueryClient();
  const { display, name: username, keyDerived, setDisplay, setName: setUsername } = createIdentity();
  const [email, setEmail] = createSignal("");
  const [password, setPassword] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);
  // A password-policy rejection from the server (the denylist) is shown inline under
  // the password field, not in the head-of-form alert, so it reads like the client checks.
  const [pwServerError, setPwServerError] = createSignal<string | null>(null);

  useFormActions().bind({
    submitLabel: "Create user",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => !username().trim() || !!handleError(username()) || !!emailError(email()) || !!passwordError(password(), username()),
    cancel: props.close,
  });

  async function submit() {
    setBusy(true);
    setErr(null);
    setPwServerError(null);
    try {
      const created = await createPrincipal({
        username: username().trim(),
        display_name: display().trim() || undefined,
        email: email().trim() || undefined,
        password: password() || undefined,
      });
      // Seed the new user's detail cache so its blade opens instantly, and flag it to
      // open in edit mode so grants can be assigned right away, then hand it to select.
      qc.setQueryData([...PRINCIPALS_KEY, created.id], created);
      openPrincipalInEdit(created.id);
      await qc.invalidateQueries({ queryKey: PRINCIPALS_KEY });
      props.onCreated(created);
    } catch (er) {
      const msg = describeError(er);
      if (isPasswordPolicyMessage(msg)) setPwServerError(msg);
      else setErr(msg);
    } finally {
      setBusy(false);
    }
  }

  return (
    <form class="flex flex-col gap-3" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <p class="text-xs text-base-content/50">Creates a human principal. Assign roles afterwards; a user with no grants can sign in but has no permissions.</p>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-display">Display name</label>
        <input id="new-display" autocomplete="off" class="input input-bordered w-full" value={display()} placeholder="Jordan Rivera" onInput={(e) => setDisplay(e.currentTarget.value)} disabled={busy()} />
      </div>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-username">Username</label>
        <input id="new-username" autocomplete="off" class="input input-bordered w-full font-data" classList={{ "input-error": !!handleError(username()) }} value={username()} placeholder="jordan" onInput={(e) => setUsername(e.currentTarget.value)} disabled={busy()} required />
        <Show when={handleError(username())} fallback={<p class="mt-1 text-[11px] text-base-content/40">{keyDerived() ? "Derived from the display name. Edit to set your own." : "What they sign in with."}</p>}>
          {(msg) => <p class="mt-1 text-[11px] text-error">{msg()}</p>}
        </Show>
      </div>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-email">Email</label>
        <input id="new-email" type="email" autocomplete="off" class="input input-bordered w-full" classList={{ "input-error": !!emailError(email()) }} value={email()} placeholder="jordan@example.com" onInput={(e) => setEmail(e.currentTarget.value)} disabled={busy()} />
        <Show when={emailError(email())}>{(msg) => <p class="mt-1 text-[11px] text-error">{msg()}</p>}</Show>
      </div>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-password">Initial password</label>
        <PasswordField id="new-password" value={password()} onInput={(v) => { setPassword(v); setPwServerError(null); }} username={username()} disabled={busy()} serverError={pwServerError()} generate />
        <p class="mt-1 text-[11px] text-base-content/40">Optional. At least 12 characters. The user changes it after signing in.</p>
      </div>
    </form>
  );
}
