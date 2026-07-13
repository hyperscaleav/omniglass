import { Show, For, createSignal, createEffect, createResource } from "solid-js";
import PasswordField from "../components/PasswordField";
import Button from "../components/Button";
import Drawer, { DrawerFooter } from "../components/Drawer";
import SessionsList from "../components/SessionsList";
import { passwordError, isPasswordPolicyMessage } from "../lib/validate";
import { useMe, useUpdateProfile, useChangePassword, setMyAvatar, removeMyAvatar, fetchMyAvatar } from "../lib/auth";
import { useSessions, useRevokeSession, useRevokeAllSelfSessions, createSelfToken, type Session } from "../lib/sessions";
import { Check, Copy, Key, LogOut, Plus, Save, Trash, X } from "../components/icons";

// Profile is the signed-in operator's own account surface: edit your display name
// and email, change your password, and (pedagogically) see the identity model you
// operate under, your principal, its permissions, and its role grants. Every field
// here is self-scoped; the server edits only the caller's own principal.
export default function Profile() {
  const me = useMe();
  const updateProfile = useUpdateProfile();
  const changePassword = useChangePassword();
  const sessions = useSessions();
  const revokeSession = useRevokeSession();
  const revokeAllSelf = useRevokeAllSelfSessions();

  // Sessions and API tokens are both bearer credentials, split by their kind so each
  // renders in its own section: a session is a web login, a token one you minted for
  // the CLI or API.
  const sessionRows = () => (sessions.data ?? []).filter((s) => s.kind === "session");
  const tokenRows = () => (sessions.data ?? []).filter((s) => s.kind === "token");

  // The id currently being revoked, so only that row's button spins.
  const [revoking, setRevoking] = createSignal<string | null>(null);
  async function revoke(s: Session) {
    setRevoking(s.id);
    const r = await revokeSession(s.id);
    // Revoking the current session signs it out: the auth guard redirects on the
    // /auth/me invalidation, so no page-level note is needed. Only a failure surfaces.
    if (!r.ok) setProfileMsg({ tone: "error", text: r.message });
    setRevoking(null);
  }
  // Bulk-revoke all of your own sessions or tokens. The server keeps the credential you
  // are on, so this never signs you out. Confirmed, since it ends everything of that kind.
  const [revokingAll, setRevokingAll] = createSignal<"session" | "token" | null>(null);
  async function revokeAll(purpose: "session" | "token") {
    const noun = purpose === "session" ? "other sessions" : "API tokens";
    if (!window.confirm(`Revoke all your ${noun}? You stay signed in on this one.`)) return;
    setRevokingAll(purpose);
    const r = await revokeAllSelf(purpose);
    if (!r.ok) setProfileMsg({ tone: "error", text: r.message });
    setRevokingAll(null);
  }

  // Create an API token for yourself: a required description and an optional lifetime.
  // The secret comes back once, shown copyable in the drawer; the list then refreshes.
  const [tokenOpen, setTokenOpen] = createSignal(false);
  const [tokenDesc, setTokenDesc] = createSignal("");
  const [tokenTtl, setTokenTtl] = createSignal<number | "">(90);
  const [tokenBusy, setTokenBusy] = createSignal(false);
  const [tokenErr, setTokenErr] = createSignal<string | null>(null);
  const [newToken, setNewToken] = createSignal<string | null>(null);
  function openCreateToken() {
    setTokenDesc("");
    setTokenTtl(90);
    setTokenErr(null);
    setNewToken(null);
    setTokenOpen(true);
  }
  async function createToken(e: SubmitEvent) {
    e.preventDefault();
    setTokenBusy(true);
    setTokenErr(null);
    const ttl = typeof tokenTtl() === "number" ? (tokenTtl() as number) : undefined;
    const r = await createSelfToken(tokenDesc().trim(), ttl);
    if (r.ok) {
      setNewToken(r.token);
      await sessions.refetch?.();
    } else {
      setTokenErr(r.message);
    }
    setTokenBusy(false);
  }
  async function copyToken() {
    const t = newToken();
    if (t) await navigator.clipboard.writeText(t);
  }

  // Seed the editable field once, when /auth/me first resolves, so later keystrokes
  // are not clobbered by the query settling.
  const [displayName, setDisplayName] = createSignal("");
  const [seeded, setSeeded] = createSignal(false);
  createEffect(() => {
    const h = me.data?.human;
    if (h && !seeded()) {
      setDisplayName(h.display_name ?? "");
      setSeeded(true);
    }
  });

  const [profileMsg, setProfileMsg] = createSignal<Note>(null);
  const [profileBusy, setProfileBusy] = createSignal(false);
  async function saveProfile(e: SubmitEvent) {
    e.preventDefault();
    setProfileBusy(true);
    setProfileMsg(null);
    const r = await updateProfile({ display_name: displayName().trim() });
    setProfileMsg(r.ok ? { tone: "success", text: "Profile saved." } : { tone: "error", text: r.message });
    setProfileBusy(false);
  }

  // The avatar preview: the first two letters of the display name being typed, or of
  // the username when it is blank, matching the sidebar avatar. A live preview of how
  // you appear (a real image lands later, see the profile-picture issue).
  const initials = () => (displayName().trim() || human()?.username || "").slice(0, 2).toUpperCase();

  const [current, setCurrent] = createSignal("");
  const [next, setNext] = createSignal("");
  const [confirm, setConfirm] = createSignal("");
  const [pwMsg, setPwMsg] = createSignal<Note>(null);
  const [pwBusy, setPwBusy] = createSignal(false);
  // A password-policy rejection (the server denylist) renders inline under the new
  // password field, like the client checks; other messages stay in the card note.
  const [pwFieldError, setPwFieldError] = createSignal<string | null>(null);
  // The change-password form lives in a slide-over so the page stays a compact
  // Profile + Access, rather than a third stacked form.
  const [pwOpen, setPwOpen] = createSignal(false);
  async function savePassword(e: SubmitEvent) {
    e.preventDefault();
    if (next() !== confirm()) {
      setPwMsg({ tone: "error", text: "The new passwords do not match." });
      return;
    }
    setPwBusy(true);
    setPwMsg(null);
    setPwFieldError(null);
    const r = await changePassword(current(), next());
    if (r.ok) {
      setCurrent("");
      setNext("");
      setConfirm("");
      setPwMsg(null);
      setPwOpen(false);
      // Feedback lands on the page (the drawer just closed).
      setProfileMsg({ tone: "success", text: "Password changed." });
    } else if (isPasswordPolicyMessage(r.message)) {
      setPwFieldError(r.message);
    } else {
      setPwMsg({ tone: "error", text: r.message });
    }
    setPwBusy(false);
  }

  const human = () => me.data?.human;

  // The avatar image, fetched as base64 and wrapped in a data URL, only when the
  // principal actually has one (has_avatar rides on /auth/me, so no fetch fires
  // for a user without a picture). A change refetches both the flag and the image.
  const [avatarUrl, { refetch: refetchAvatar }] = createResource(
    () => human()?.has_avatar ?? false,
    (has) => (has ? fetchMyAvatar() : Promise.resolve(null)),
  );
  const [avatarMsg, setAvatarMsg] = createSignal<Note>(null);
  let avatarFileInput: HTMLInputElement | undefined;

  async function onPickAvatar(e: Event) {
    const file = (e.currentTarget as HTMLInputElement).files?.[0];
    if (!file) return;
    const r = await setMyAvatar(file);
    if (r.ok) {
      await me.refetch?.(); // refresh has_avatar so the flag and image agree
      await refetchAvatar();
      setAvatarMsg({ tone: "success", text: "Profile picture updated." });
    } else {
      setAvatarMsg({ tone: "error", text: r.message });
    }
  }

  async function onRemoveAvatar() {
    const r = await removeMyAvatar();
    if (r.ok) {
      await me.refetch?.();
      await refetchAvatar();
      setAvatarMsg(null);
    } else {
      setAvatarMsg({ tone: "error", text: r.message });
    }
  }

  return (
    <section class="og-stack flex flex-col">
      <div class="grid gap-4">
        {/* Profile card */}
        <form onSubmit={saveProfile} class="card border border-base-300 bg-base-200">
          <div class="card-body gap-3">
            <h2 class="card-title text-base">Profile</h2>
            {/* Avatar: the profile picture when set, else initials from the display name. */}
            <div class="flex items-center gap-3">
              <Show
                when={avatarUrl()}
                fallback={
                  <div class="avatar avatar-placeholder">
                    <div class="w-16 rounded-full bg-linear-to-br from-primary to-info text-primary-content">
                      <span class="font-data text-lg font-bold uppercase">{initials()}</span>
                    </div>
                  </div>
                }
              >
                <div class="avatar">
                  <div class="w-16 rounded-full">
                    <img src={avatarUrl()!} alt="Your profile picture" />
                  </div>
                </div>
              </Show>
              <div class="flex flex-col gap-1">
                <input
                  type="file"
                  accept="image/jpeg,image/png,image/webp"
                  class="hidden"
                  ref={avatarFileInput}
                  onChange={onPickAvatar}
                />
                <Button size="sm" onClick={() => avatarFileInput?.click()}>Upload</Button>
                <Show when={human()?.has_avatar}>
                  <Button size="sm" intent="danger" onClick={onRemoveAvatar}>Remove</Button>
                </Show>
              </div>
            </div>
            <Note note={avatarMsg()} />
            <div>
              <label class="eyebrow mb-1.5 block">Username</label>
              <input type="text" class="input input-bordered w-full" value={human()?.username ?? ""} disabled readonly />
              <p class="mt-1 text-[11px] text-base-content/40">Your sign-in name. An administrator can change it.</p>
            </div>
            <div>
              <label class="eyebrow mb-1.5 block" for="profile-display-name">Display name</label>
              <input
                id="profile-display-name"
                type="text"
                class="input input-bordered w-full"
                value={displayName()}
                onInput={(e) => setDisplayName(e.currentTarget.value)}
                disabled={profileBusy()}
              />
            </div>
            <div>
              <label class="eyebrow mb-1.5 block">Email</label>
              <input type="email" class="input input-bordered w-full" value={human()?.email ?? ""} disabled readonly placeholder="not set" />
              <p class="mt-1 text-[11px] text-base-content/40">An administrator sets your email.</p>
            </div>
            <Note note={profileMsg()} />
            <div class="card-actions mt-1 justify-between border-t border-base-300 pt-3">
              <Button icon={Key} onClick={() => setPwOpen(true)}>Change password</Button>
              <Button type="submit" intent="action" icon={Save} loading={profileBusy()}>Save profile</Button>
            </div>
          </div>
        </form>

        {/* Access: read-only, teaches the identity model this page operates under. */}
        <div class="card border border-base-300 bg-base-200">
          <div class="card-body gap-3">
            <h2 class="card-title text-base">Access</h2>
            <p class="text-xs text-base-content/50">
              You are a <span class="font-data text-base-content/70">{me.data?.principal.kind}</span> principal. Your
              permissions are the flattened union of the roles granted to you; the server enforces them on every request
              (this is a hint, not the authority).
            </p>
            <div class="grid gap-3 sm:grid-cols-2">
              <div>
                <div class="eyebrow mb-1.5">Role grants</div>
                <div class="flex flex-wrap gap-1.5">
                  <For each={me.data?.grants ?? []} fallback={<span class="text-xs text-base-content/40">none</span>}>
                    {(g) => (
                      <span class="badge badge-soft badge-primary font-data text-[11px]">
                        {g.role} @ {g.scope_kind}{g.scope_id ? `:${g.scope_id}` : ""}
                      </span>
                    )}
                  </For>
                </div>
              </div>
              <div>
                <div class="eyebrow mb-1.5">Permissions ({me.data?.permissions.length ?? 0})</div>
                <div class="flex flex-wrap gap-1.5">
                  <For each={me.data?.permissions ?? []} fallback={<span class="text-xs text-base-content/40">none</span>}>
                    {(p) => <span class="badge badge-ghost font-data text-[11px]">{p}</span>}
                  </For>
                </div>
              </div>
            </div>
          </div>
        </div>

        {/* Sessions: the browsers and devices the caller is signed in from, each revocable. */}
        <div class="card border border-base-300 bg-base-200">
          <div class="card-body gap-3">
            <div class="flex items-center justify-between gap-2">
              <h2 class="card-title text-base">Sessions</h2>
              <Show when={sessionRows().length > 1}>
                <Button intent="danger" size="xs" icon={LogOut} loading={revokingAll() === "session"} onClick={() => revokeAll("session")}>Revoke all</Button>
              </Show>
            </div>
            <p class="text-xs text-base-content/50">
              Devices and browsers you are signed in from. Each <span class="font-data text-base-content/70">session</span>
              {" "}expires on its own; revoke anything you do not recognize, and revoking the one you are using signs you out.
              The credential secret is never shown here, only its <span class="font-data text-base-content/70">ogp_</span> locator.
            </p>
            <Show when={sessions.error}>
              <div role="alert" class="alert alert-error alert-soft text-sm"><span>Could not load your sessions.</span></div>
            </Show>
            <SessionsList sessions={sessionRows()} revoking={revoking()} onRevoke={revoke} emptyLabel="No active sessions." />
          </div>
        </div>

        {/* API tokens: credentials the caller minted for the CLI or API, each revocable. */}
        <div class="card border border-base-300 bg-base-200">
          <div class="card-body gap-3">
            <div class="flex items-center justify-between gap-2">
              <h2 class="card-title text-base">API tokens</h2>
              <div class="flex items-center gap-2">
                <Show when={tokenRows().length > 0}>
                  <Button intent="danger" size="xs" icon={Trash} loading={revokingAll() === "token"} onClick={() => revokeAll("token")}>Revoke all</Button>
                </Show>
                <Button intent="action" size="xs" icon={Plus} onClick={openCreateToken}>Create token</Button>
              </div>
            </div>
            <p class="text-xs text-base-content/50">
              Tokens you minted for the CLI or API. Each <span class="font-data text-base-content/70">token</span> is
              time-bounded and expires on its own; revoke any you no longer use. The token secret is never shown here, only
              its <span class="font-data text-base-content/70">ogp_</span> locator.
            </p>
            <Show when={sessions.error}>
              <div role="alert" class="alert alert-error alert-soft text-sm"><span>Could not load your tokens.</span></div>
            </Show>
            <SessionsList sessions={tokenRows()} revoking={revoking()} onRevoke={revoke} emptyLabel="No API tokens." />
          </div>
        </div>
      </div>

      <Drawer open={pwOpen()} onClose={() => setPwOpen(false)} title="Change password">
        <form onSubmit={savePassword} class="flex min-h-full flex-col gap-3">
          <div>
            <label class="eyebrow mb-1.5 block" for="pw-current">Current password</label>
            <input
              id="pw-current"
              type="password"
              autocomplete="current-password"
              class="input input-bordered w-full font-data"
              value={current()}
              onInput={(e) => setCurrent(e.currentTarget.value)}
              disabled={pwBusy()}
              required
            />
          </div>
          <div>
            <label class="eyebrow mb-1.5 block" for="pw-new">New password</label>
            <PasswordField id="pw-new" value={next()} onInput={(v) => { setNext(v); setPwFieldError(null); }} username={human()?.username} disabled={pwBusy()} serverError={pwFieldError()} required generate />
            <p class="mt-1 text-[11px] text-base-content/40">At least 12 characters, not a common password.</p>
          </div>
          <div>
            <label class="eyebrow mb-1.5 block" for="pw-confirm">Confirm new password</label>
            <input
              id="pw-confirm"
              type="password"
              autocomplete="new-password"
              class="input input-bordered w-full font-data"
              value={confirm()}
              onInput={(e) => setConfirm(e.currentTarget.value)}
              disabled={pwBusy()}
              required
            />
            <Show when={confirm() && next() !== confirm()}>
              <p class="mt-1 text-[11px] text-error">Passwords do not match.</p>
            </Show>
          </div>
          <Note note={pwMsg()} />
          <DrawerFooter>
            <Button icon={X} onClick={() => setPwOpen(false)} disabled={pwBusy()}>Cancel</Button>
            <Button type="submit" intent="action" icon={Save} loading={pwBusy()} disabled={!current() || !next() || next() !== confirm() || !!passwordError(next(), human()?.username)}>Change password</Button>
          </DrawerFooter>
        </form>
      </Drawer>

      <Drawer open={tokenOpen()} onClose={() => setTokenOpen(false)} title="Create API token">
        <Show
          when={newToken()}
          fallback={
            <form onSubmit={createToken} class="flex min-h-full flex-col gap-3">
              <div>
                <label class="eyebrow mb-1.5 block" for="tok-desc">Description</label>
                <input
                  id="tok-desc"
                  class="input input-bordered w-full"
                  placeholder="What is this token for? (e.g. CI pipeline)"
                  value={tokenDesc()}
                  onInput={(e) => setTokenDesc(e.currentTarget.value)}
                  disabled={tokenBusy()}
                  required
                />
                <p class="mt-1 text-[11px] text-base-content/40">Required, so you can tell your tokens apart later.</p>
              </div>
              <div>
                <label class="eyebrow mb-1.5 block" for="tok-ttl">Expires in (days)</label>
                <input
                  id="tok-ttl"
                  type="number"
                  min="1"
                  max="365"
                  class="input input-bordered w-full font-data"
                  value={tokenTtl()}
                  onInput={(e) => setTokenTtl(e.currentTarget.value === "" ? "" : Number(e.currentTarget.value))}
                  disabled={tokenBusy()}
                />
                <p class="mt-1 text-[11px] text-base-content/40">Default 90, maximum 365. Every token is time-bounded.</p>
              </div>
              <Show when={tokenErr()}>
                <div role="alert" class="alert alert-error alert-soft text-sm"><span>{tokenErr()}</span></div>
              </Show>
              <DrawerFooter>
                <Button icon={X} onClick={() => setTokenOpen(false)} disabled={tokenBusy()}>Cancel</Button>
                <Button type="submit" intent="action" icon={Plus} loading={tokenBusy()} disabled={!tokenDesc().trim()}>Create token</Button>
              </DrawerFooter>
            </form>
          }
        >
          <div class="flex min-h-full flex-col gap-3">
            <div role="alert" class="alert alert-success alert-soft text-sm">
              <span>Token created. Copy it now: for your security it is not shown again.</span>
            </div>
            <div>
              <label class="eyebrow mb-1.5 block" for="tok-secret">Your new token</label>
              <div class="flex gap-2">
                <input id="tok-secret" readonly class="input input-bordered w-full font-data text-xs" value={newToken()!} />
                <Button icon={Copy} onClick={copyToken}>Copy</Button>
              </div>
            </div>
            <DrawerFooter>
              <Button intent="action" icon={Check} onClick={() => setTokenOpen(false)}>Done</Button>
            </DrawerFooter>
          </div>
        </Show>
      </Drawer>
    </section>
  );
}

type Note = { tone: "success" | "error"; text: string } | null;

// Note renders a soft success/error alert, or nothing.
function Note(props: { note: Note }) {
  return (
    <Show when={props.note}>
      <div
        role="alert"
        class="alert alert-soft text-sm"
        classList={{ "alert-success": props.note!.tone === "success", "alert-error": props.note!.tone === "error" }}
      >
        <span>{props.note!.text}</span>
      </div>
    </Show>
  );
}
