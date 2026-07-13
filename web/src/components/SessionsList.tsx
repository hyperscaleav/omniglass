import { Show, For } from "solid-js";
import Button from "./Button";
import { Trash, LogOut } from "./icons";
import { rel, fmtTime } from "../lib/format";
import type { Session } from "../lib/sessions";

// SessionsList renders one list of the caller's own bearer credentials (already
// filtered to a single kind by the caller), each row showing its ogp_ locator, when
// it was started and when it expires, and a revoke action. It is the shared primitive
// behind both the Sessions and the API tokens sections of the profile: the two differ
// only in which credentials they pass and their empty-state label. Every credential is
// time-bounded now, so a row always shows an expiry. The current credential is flagged
// and its revoke reads as a sign-out. onRevoke is optional: without it the list is
// read-only (an admin can see, but not end, an owner's credentials).
export default function SessionsList(props: {
  sessions: Session[];
  revoking?: string | null;
  onRevoke?: (s: Session) => void;
  emptyLabel: string;
}) {
  return (
    <ul class="flex flex-col divide-y divide-base-300">
      <For each={props.sessions} fallback={<li class="py-2 text-xs text-base-content/40">{props.emptyLabel}</li>}>
        {(s) => (
          <li class="flex items-center gap-3 py-2.5">
            <span class="badge badge-soft badge-sm" classList={{ "badge-primary": s.kind === "session", "badge-ghost": s.kind === "token" }}>{s.kind}</span>
            <div class="min-w-0 flex-1 leading-tight">
              <div class="flex items-center gap-2">
                <span class="truncate font-data text-xs text-base-content/70">ogp_{s.prefix}</span>
                <Show when={s.current}><span class="badge badge-soft badge-success badge-xs flex-none">This session</span></Show>
              </div>
              <div class="text-[11px] text-base-content/40">
                Started {rel(s.created_at)}{s.expires_at ? ` · expires ${fmtTime(s.expires_at)}` : ""}
              </div>
            </div>
            <Show when={props.onRevoke}>
              <Show
                when={s.current}
                fallback={<Button intent="danger" size="xs" icon={Trash} loading={props.revoking === s.id} onClick={() => props.onRevoke!(s)}>Revoke</Button>}
              >
                <Button intent="danger" size="xs" icon={LogOut} loading={props.revoking === s.id} onClick={() => props.onRevoke!(s)}>Sign out</Button>
              </Show>
            </Show>
          </li>
        )}
      </For>
    </ul>
  );
}
