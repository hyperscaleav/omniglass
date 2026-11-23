import { Show, For } from "solid-js";
import Button from "./Button";
import { Trash } from "./icons";
import { rel } from "../lib/format";
import type { IdentityLink } from "../lib/identityLinks";

// The row's secondary line: who made the link, when, and when it last signed
// someone in, joined by dots, skipping any part that is absent.
function meta(l: IdentityLink): string {
  return [
    l.self ? "Linked by the person" : l.linked_by ? `Linked by ${l.linked_by}` : "Linked by a removed user",
    rel(l.created_at),
    l.last_used_at ? `last sign-in ${rel(l.last_used_at)}` : "never used",
    l.refresh_accepted_at ? `provider re-checked ${rel(l.refresh_accepted_at)}` : null,
  ]
    .filter(Boolean)
    .join(" · ");
}

// IdentityLinksList renders a person's identity links (#2362): each is an
// identity at the identity provider, shown as the two halves of its iss|sub
// identifier, with who made it and when it was last used. The shared primitive
// behind the admin blade and the profile; onUnlink is optional, without it the
// list is read-only (an owner's links on another admin's screen).
export default function IdentityLinksList(props: {
  links: IdentityLink[];
  unlinking?: string | null;
  onUnlink?: (l: IdentityLink) => void;
  emptyLabel: string;
}) {
  return (
    <ul class="flex flex-col divide-y divide-base-300">
      <For each={props.links} fallback={<li class="py-2 text-xs text-base-content/40">{props.emptyLabel}</li>}>
        {(l) => (
          <li class="flex items-center gap-3 py-2.5">
            <span class="badge badge-soft badge-primary badge-sm">oidc</span>
            <div class="min-w-0 flex-1 leading-tight">
              <div class="flex min-w-0 items-center gap-2">
                <span class="truncate font-data text-xs text-base-content/80" title={l.identifier}>{l.subject}</span>
                <span class="truncate font-data text-[11px] text-base-content/40" title="The issuer: the identity provider that vouches for this subject">{l.issuer}</span>
              </div>
              <div class="text-[11px] text-base-content/40">{meta(l)}</div>
            </div>
            <Show when={props.onUnlink}>
              <Button intent="danger" size="xs" icon={Trash} loading={props.unlinking === l.id} onClick={() => props.onUnlink!(l)}>Unlink</Button>
            </Show>
          </li>
        )}
      </For>
    </ul>
  );
}
