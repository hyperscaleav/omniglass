import { Show, For, createSignal, createEffect } from "solid-js";
import Page from "../components/Page";
import { useMe, useUpdateProfile, useChangePassword } from "../lib/auth";

// Profile is the signed-in operator's own account surface: edit your display name
// and email, change your password, and (pedagogically) see the identity model you
// operate under, your principal, its permissions, and its role grants. Every field
// here is self-scoped; the server edits only the caller's own principal.
export default function Profile() {
  const me = useMe();
  const updateProfile = useUpdateProfile();
  const changePassword = useChangePassword();

  // Seed the editable fields once, when /auth/me first resolves, so later keystrokes
  // are not clobbered by the query settling.
  const [displayName, setDisplayName] = createSignal("");
  const [email, setEmail] = createSignal("");
  const [seeded, setSeeded] = createSignal(false);
  createEffect(() => {
    const h = me.data?.human;
    if (h && !seeded()) {
      setDisplayName(h.display_name ?? "");
      setEmail(h.email ?? "");
      setSeeded(true);
    }
  });

  const [profileMsg, setProfileMsg] = createSignal<Note>(null);
  const [profileBusy, setProfileBusy] = createSignal(false);
  async function saveProfile(e: SubmitEvent) {
    e.preventDefault();
    setProfileBusy(true);
    setProfileMsg(null);
    const r = await updateProfile({ display_name: displayName().trim(), email: email().trim() });
    setProfileMsg(r.ok ? { tone: "success", text: "Profile saved." } : { tone: "error", text: r.message });
    setProfileBusy(false);
  }

  const [current, setCurrent] = createSignal("");
  const [next, setNext] = createSignal("");
  const [confirm, setConfirm] = createSignal("");
  const [pwMsg, setPwMsg] = createSignal<Note>(null);
  const [pwBusy, setPwBusy] = createSignal(false);
  async function savePassword(e: SubmitEvent) {
    e.preventDefault();
    if (next() !== confirm()) {
      setPwMsg({ tone: "error", text: "The new passwords do not match." });
      return;
    }
    setPwBusy(true);
    setPwMsg(null);
    const r = await changePassword(current(), next());
    if (r.ok) {
      setPwMsg({ tone: "success", text: "Password changed." });
      setCurrent("");
      setNext("");
      setConfirm("");
    } else {
      setPwMsg({ tone: "error", text: r.message });
    }
    setPwBusy(false);
  }

  const human = () => me.data?.human;

  return (
    <Page title="Your profile">
      <div class="grid max-w-4xl gap-4 lg:grid-cols-2">
        {/* Profile card */}
        <form onSubmit={saveProfile} class="card border border-base-300 bg-base-200">
          <div class="card-body gap-3">
            <h2 class="card-title text-base">Profile</h2>
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
              <label class="eyebrow mb-1.5 block" for="profile-email">Email</label>
              <input
                id="profile-email"
                type="email"
                autocomplete="email"
                class="input input-bordered w-full"
                value={email()}
                onInput={(e) => setEmail(e.currentTarget.value)}
                disabled={profileBusy()}
              />
            </div>
            <Note note={profileMsg()} />
            <div class="card-actions">
              <button type="submit" class="btn btn-primary btn-sm" disabled={profileBusy()}>
                <Show when={profileBusy()}><span class="loading loading-spinner loading-xs" /></Show>
                Save profile
              </button>
            </div>
          </div>
        </form>

        {/* Change-password card */}
        <form onSubmit={savePassword} class="card border border-base-300 bg-base-200">
          <div class="card-body gap-3">
            <h2 class="card-title text-base">Change password</h2>
            <div>
              <label class="eyebrow mb-1.5 block" for="pw-current">Current password</label>
              <input
                id="pw-current"
                type="password"
                autocomplete="current-password"
                class="input input-bordered w-full"
                value={current()}
                onInput={(e) => setCurrent(e.currentTarget.value)}
                disabled={pwBusy()}
                required
              />
            </div>
            <div>
              <label class="eyebrow mb-1.5 block" for="pw-new">New password</label>
              <input
                id="pw-new"
                type="password"
                autocomplete="new-password"
                minLength={8}
                class="input input-bordered w-full"
                value={next()}
                onInput={(e) => setNext(e.currentTarget.value)}
                disabled={pwBusy()}
                required
              />
              <p class="mt-1 text-[11px] text-base-content/40">At least 8 characters.</p>
            </div>
            <div>
              <label class="eyebrow mb-1.5 block" for="pw-confirm">Confirm new password</label>
              <input
                id="pw-confirm"
                type="password"
                autocomplete="new-password"
                class="input input-bordered w-full"
                value={confirm()}
                onInput={(e) => setConfirm(e.currentTarget.value)}
                disabled={pwBusy()}
                required
              />
            </div>
            <Note note={pwMsg()} />
            <div class="card-actions">
              <button type="submit" class="btn btn-primary btn-sm" disabled={pwBusy() || !current() || !next()}>
                <Show when={pwBusy()}><span class="loading loading-spinner loading-xs" /></Show>
                Change password
              </button>
            </div>
          </div>
        </form>

        {/* Access: read-only, teaches the identity model this page operates under. */}
        <div class="card border border-base-300 bg-base-200 lg:col-span-2">
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
      </div>
    </Page>
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
