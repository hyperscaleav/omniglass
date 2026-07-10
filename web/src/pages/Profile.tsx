import { Show, For, createSignal, createEffect } from "solid-js";
import PasswordField from "../components/PasswordField";
import Button from "../components/Button";
import Drawer, { DrawerFooter } from "../components/Drawer";
import { passwordError, isPasswordPolicyMessage } from "../lib/validate";
import { useMe, useUpdateProfile, useChangePassword } from "../lib/auth";
import { useSessions, useRevokeSession, type Session } from "../lib/sessions";
import { rel, fmtTime } from "../lib/format";
import { Key, Save, X, Trash, LogOut } from "../components/icons";

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

  return (
    <section class="og-stack flex flex-col">
      <div class="grid gap-4">
        {/* Profile card */}
        <form onSubmit={saveProfile} class="card border border-base-300 bg-base-200">
          <div class="card-body gap-3">
            <h2 class="card-title text-base">Profile</h2>
            {/* Avatar preview: initials from the display name being typed. */}
            <div class="flex items-center gap-3">
              <div class="avatar avatar-placeholder">
                <div class="w-12 rounded-full bg-linear-to-br from-primary to-info text-primary-content">
                  <span class="font-data text-sm font-bold uppercase">{initials()}</span>
                </div>
              </div>
              <div class="min-w-0 leading-tight">
                <div class="truncate font-data text-sm font-semibold">{displayName().trim() || human()?.username}</div>
                <div class="text-[11px] text-base-content/40">This is how you appear in the console.</div>
              </div>
            </div>
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

        {/* Sessions: the caller's own active sign-ins and API tokens, each revocable. */}
        <div class="card border border-base-300 bg-base-200">
          <div class="card-body gap-3">
            <h2 class="card-title text-base">Sessions</h2>
            <p class="text-xs text-base-content/50">
              Each device you sign in from is a <span class="font-data text-base-content/70">session</span> that expires on
              its own; a credential you mint for the CLI or API is a <span class="font-data text-base-content/70">token</span>
              {" "}that does not. Revoke anything you do not recognize; revoking the one you are using signs you out. The
              token secret is never shown here, only its <span class="font-data text-base-content/70">ogp_</span> locator.
            </p>
            <Show when={sessions.error}>
              <div role="alert" class="alert alert-error alert-soft text-sm"><span>Could not load your sessions.</span></div>
            </Show>
            <ul class="flex flex-col divide-y divide-base-300">
              <For each={sessions.data ?? []} fallback={<li class="py-2 text-xs text-base-content/40">No active sessions.</li>}>
                {(s) => (
                  <li class="flex items-center gap-3 py-2.5">
                    <span class="badge badge-soft badge-sm" classList={{ "badge-primary": s.kind === "session", "badge-ghost": s.kind === "token" }}>{s.kind}</span>
                    <div class="min-w-0 flex-1 leading-tight">
                      <div class="flex items-center gap-2">
                        <span class="truncate font-data text-xs text-base-content/70">ogp_{s.prefix}</span>
                        <Show when={s.current}><span class="badge badge-soft badge-success badge-xs flex-none">This session</span></Show>
                      </div>
                      <div class="text-[11px] text-base-content/40">
                        Started {rel(s.created_at)} · {s.expires_at ? `expires ${fmtTime(s.expires_at)}` : "never expires"}
                      </div>
                    </div>
                    <Show
                      when={s.current}
                      fallback={<Button intent="danger" size="xs" icon={Trash} loading={revoking() === s.id} onClick={() => revoke(s)}>Revoke</Button>}
                    >
                      <Button intent="danger" size="xs" icon={LogOut} loading={revoking() === s.id} onClick={() => revoke(s)}>Sign out</Button>
                    </Show>
                  </li>
                )}
              </For>
            </ul>
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
