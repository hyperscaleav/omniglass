import { For, Show, createSignal, type JSX } from "solid-js";
import { A, useNavigate } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import Button from "../components/Button";
import InfoTip from "../components/InfoTip";
import { Bell, Check, Plus, RotateCcw } from "../components/icons";
import { can, useMe } from "../lib/auth";
import { describeError, fmtTime, rel } from "../lib/format";
import { INBOX_PAGE, inboxEntryHref, inboxListKey, listInbox, useInboxMarks, type InboxEntry } from "../lib/inbox";

// The console inbox (#1355, epic #1341): the notifications addressed to the
// signed-in person on the inbox channel, newest first. Each entry says what
// happened (its rendered subject), on which component (a link to that
// component's Activity, where the alarm sits), which edge (raised or cleared)
// and when, and whether it has been read. One entry or all of them can be
// marked read; either refreshes the bell in the top bar, which shares this
// page's cache prefix. The page reads only what the server lets the caller
// read: their own entries, about components their read scope admits now.
const HINTS = {
  inbox:
    "Notifications addressed to you on the inbox channel. You are only ever told about alarms on components you can read, and an entry about a component you can no longer read leaves your inbox.",
  group:
    "An administrator points an action rule at you, or at a group you belong to, with the inbox as its channel. If the rule allows it, you can opt out of it for yourself.",
  personal:
    "Your own alarm subscription: a rule you write that tells you alone, on the channels you pick, when an alarm you can see is raised or cleared.",
  unreadOnly: "Show only the entries you have not read yet.",
};

export default function Inbox(): JSX.Element {
  const me = useMe();
  const navigate = useNavigate();
  const marks = useInboxMarks();
  const canRead = () => can(me.data, "notification", "read");
  const canSubscribe = () => can(me.data, "notification", "subscribe");
  const [unreadOnly, setUnreadOnly] = createSignal(false);
  const inbox = useQuery(() => ({
    queryKey: inboxListKey(unreadOnly()),
    queryFn: () => listInbox({ unread: unreadOnly(), limit: INBOX_PAGE }),
    enabled: canRead(),
    retry: false,
  }));
  const entries = () => inbox.data?.notifications ?? [];
  const unread = () => inbox.data?.unread ?? 0;

  // The id being marked, so only that row's button spins; "all" for the bulk.
  const [marking, setMarking] = createSignal<string | null>(null);
  const [markErr, setMarkErr] = createSignal<string | null>(null);
  async function markOne(e: InboxEntry) {
    setMarking(e.id);
    setMarkErr(null);
    try {
      await marks.one(e.id);
    } catch (err) {
      setMarkErr(describeError(err));
    }
    setMarking(null);
  }
  async function markAll() {
    setMarking("all");
    setMarkErr(null);
    try {
      await marks.all();
    } catch (err) {
      setMarkErr(describeError(err));
    }
    setMarking(null);
  }
  // Following an entry to its component reads it: the person went to look.
  function follow(e: InboxEntry) {
    if (!e.read_at) void marks.one(e.id).catch(() => {});
  }

  const title = (
    <span class="flex items-center gap-2">
      Inbox
      <InfoTip text={HINTS.inbox} label="Inbox" />
    </span>
  );

  return (
    <Page
      title={title}
      actions={
        <Show when={canRead()}>
          <Button pressed={unreadOnly()} intent={unreadOnly() ? "action" : "quiet"} title={HINTS.unreadOnly} onClick={() => setUnreadOnly((v) => !v)}>
            Unread only
          </Button>
          <Button icon={Check} loading={marking() === "all"} disabled={unread() === 0 || marking() !== null} onClick={() => void markAll()}>
            Mark all read
          </Button>
        </Show>
      }
    >
      <Show when={me.data && !canRead()}>
        <div role="alert" class="alert alert-warning alert-soft text-sm" data-testid="inbox-denied">
          <span>
            Your role does not include <span class="font-data">notification:read</span>, so there is no inbox to show. An administrator grants it.
          </span>
        </div>
      </Show>

      <Show when={canRead()}>
        <Show when={markErr()}>
          {(m) => (
            <div role="alert" class="alert alert-error alert-soft text-sm">
              <span>Could not mark read: {m()}</span>
            </div>
          )}
        </Show>

        <Show when={inbox.isPending}>
          <div class="card border border-base-300 bg-base-200 p-4" aria-busy="true" data-testid="inbox-loading">
            <For each={[0, 1, 2, 3, 4]}>{() => <div class="skeleton mb-2 h-14 w-full" />}</For>
          </div>
        </Show>

        <Show when={inbox.error && !inbox.data}>
          <div role="alert" class="alert alert-error alert-soft flex items-center justify-between" data-testid="inbox-error">
            <span>Could not read your inbox: {describeError(inbox.error)}</span>
            <Button icon={RotateCcw} loading={inbox.isFetching} onClick={() => void inbox.refetch()}>Retry</Button>
          </div>
        </Show>

        <Show when={inbox.data && entries().length === 0}>
          <Show
            when={unreadOnly()}
            fallback={
              <EmptyInbox
                canSubscribe={canSubscribe()}
                onSubscribe={() => navigate("/profile/subscriptions")}
              />
            }
          >
            <section class="card border border-base-300 bg-base-200" data-testid="inbox-caught-up">
              <div class="card-body items-center gap-3 p-10 text-center">
                <span class="text-base-content/30"><Check size={32} /></span>
                <h2 class="text-base font-semibold">Nothing unread</h2>
                <Button onClick={() => setUnreadOnly(false)}>Show everything</Button>
              </div>
            </section>
          </Show>
        </Show>

        <Show when={entries().length > 0}>
          <section class="card overflow-hidden border border-base-300 bg-base-200 p-0" data-testid="inbox-list">
            <div class="flex items-center gap-2 border-b border-base-300 px-4 py-2.5 text-xs text-base-content/60">
              <span class="eyebrow">Entries</span>
              <span class="font-data tnum">{entries().length}</span>
              <span class="text-base-content/30">/</span>
              <span class="font-data tnum" data-testid="inbox-unread-count">{unread()}</span>
              <span>unread</span>
            </div>
            <ul class="divide-y divide-base-300">
              <For each={entries()}>
                {(e) => (
                  <InboxRow entry={e} busy={marking() === e.id} disabled={marking() !== null} onMark={() => void markOne(e)} onFollow={() => follow(e)} />
                )}
              </For>
            </ul>
          </section>
        </Show>
      </Show>
    </Page>
  );
}

const TRANSITION_PILL: Record<string, string> = {
  raised: "badge badge-sm badge-soft badge-warning",
  cleared: "badge badge-sm badge-healthy",
};

function InboxRow(props: { entry: InboxEntry; busy: boolean; disabled: boolean; onMark: () => void; onFollow: () => void }) {
  const e = () => props.entry;
  const isUnread = () => !e().read_at;
  return (
    <li
      class="flex items-start gap-3 px-4 py-3"
      classList={{ "bg-base-100/40": isUnread() }}
      data-testid="inbox-entry"
      data-unread={isUnread() ? "true" : "false"}
    >
      <span class="mt-1.5 flex h-2 w-2 flex-none items-center justify-center" aria-hidden="true">
        <Show when={isUnread()}>
          <span class="h-2 w-2 rounded-full bg-primary" />
        </Show>
      </span>
      <div class="flex min-w-0 flex-1 flex-col gap-1">
        <div class="flex min-w-0 items-baseline gap-2">
          <span class="sr-only">{isUnread() ? "Unread: " : "Read: "}</span>
          <span class="truncate text-sm" classList={{ "font-semibold": isUnread(), "text-base-content/70": !isUnread() }}>
            {e().subject || "(no subject)"}
          </span>
        </div>
        <Show when={e().text}>
          <p class="line-clamp-2 whitespace-pre-line text-xs text-base-content/50">{e().text}</p>
        </Show>
        <div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-base-content/60">
          <A href={inboxEntryHref(e())} class="link link-hover font-data text-base-content/80" onClick={props.onFollow} title="Open the component's activity, where this alarm sits">
            {e().component}
          </A>
          <span class={TRANSITION_PILL[e().transition] ?? "badge badge-sm badge-ghost"}>{e().transition}</span>
          <span class="tnum" title={fmtTime(e().created_at)}>{rel(e().created_at)}</span>
          <Show when={e().action_rule}>
            <span class="text-base-content/40">
              via <span class="font-data text-base-content/60">{e().action_rule}</span>
            </span>
          </Show>
        </div>
      </div>
      <Show when={isUnread()}>
        <Button size="xs" icon={Check} loading={props.busy} disabled={props.disabled} onClick={props.onMark} label={`Mark read: ${e().subject}`}>
          Mark read
        </Button>
      </Show>
    </li>
  );
}

// The empty inbox teaches how an entry gets here: the two doors, each a chip
// whose tooltip says what it is, and the one a person opens themselves.
function EmptyInbox(props: { canSubscribe: boolean; onSubscribe: () => void }) {
  return (
    <section class="card border border-base-300 bg-base-200" data-testid="inbox-empty">
      <div class="card-body items-center gap-3 p-10 text-center">
        <span class="text-base-content/30"><Bell size={32} /></span>
        <div class="flex items-center gap-1.5">
          <h2 class="text-base font-semibold">Your inbox is empty</h2>
          <InfoTip text={HINTS.inbox} label="Your inbox is empty" />
        </div>
        <div class="flex flex-wrap items-center justify-center gap-2 text-xs text-base-content/60">
          <span>What reaches it:</span>
          <InfoTip text={HINTS.group} label="A group notification" class="badge badge-sm badge-ghost cursor-help">
            A group notification
          </InfoTip>
          <span class="text-base-content/30">or</span>
          <InfoTip text={HINTS.personal} label="Your own subscription" class="badge badge-sm badge-ghost cursor-help">
            Your own subscription
          </InfoTip>
        </div>
        <Show when={props.canSubscribe}>
          <Button intent="action" icon={Plus} onClick={props.onSubscribe}>Subscribe to alarms</Button>
        </Show>
      </div>
    </section>
  );
}
