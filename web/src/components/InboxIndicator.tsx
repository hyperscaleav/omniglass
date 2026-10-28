import { Show } from "solid-js";
import { A } from "@solidjs/router";
import { Bell } from "./icons";
import { can, useMe } from "../lib/auth";
import { badgeText, useUnreadCount } from "../lib/inbox";

// InboxIndicator is the shell's door to the console inbox (#1355): a bell in
// the top bar with the unread count on it, linking to /inbox. The count polls
// (lib/inbox INBOX_POLL_MS) and refreshes the moment an entry is marked read
// anywhere, since every inbox read shares one cache prefix. A principal who
// cannot read notifications gets no bell at all, so nothing asks a route the
// server would refuse.
export default function InboxIndicator() {
  const me = useMe();
  const count = useUnreadCount();
  const text = () => badgeText(count.data);
  const name = () => (text() ? `Inbox, ${count.data} unread` : "Inbox");
  return (
    <Show when={can(me.data, "notification", "read")}>
      <A
        href="/inbox"
        class="relative inline-flex h-8 w-8 flex-none items-center justify-center rounded-field text-base-content/60 transition-colors hover:bg-base-200 hover:text-base-content focus-visible:outline-2 focus-visible:outline-primary"
        activeClass="bg-base-200 text-base-content"
        title={name()}
        aria-label={name()}
        data-testid="inbox-indicator"
      >
        <Bell size={17} />
        <Show when={text()}>
          <span
            class="badge badge-xs badge-primary absolute -right-1.5 -top-1 h-4 min-w-4 px-1 font-data text-[10px] font-semibold tnum"
            aria-hidden="true"
            data-testid="inbox-unread"
          >
            {text()}
          </span>
        </Show>
      </A>
    </Show>
  );
}
