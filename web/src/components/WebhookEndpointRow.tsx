import { Show, createMemo, createSignal } from "solid-js";
import { ChevronRight, Copy, Eye, Sliders } from "./icons";
import Button from "./Button";
import InfoTip from "./InfoTip";
import { rel } from "../lib/format";
import { CLOCK_TICK_MS, createClock } from "../lib/clock";
import { entityLabel } from "../lib/entities";
import { can, useMe } from "../lib/auth";
import { copySecret, revealSecret } from "../lib/secrets";
import type { ReachEndpoint } from "../lib/reachability";

// A webhook endpoint's row on the component's Endpoints panel (#1024): a push
// endpoint, hosted by the server. Nothing is dialed, so it has no ladder and
// no verdict; it reads last heard (stamped by every post that lands, never
// down for silence), the scheme that checks each post (or unsigned), and what
// that scheme refused. Expanded, it shows the URL a sender posts to, the token
// masked, revealed and copied only under secret:reveal (audited).
export default function WebhookEndpointRow(p: { iface: ReachEndpoint; manageId?: string; onManage?: (id: string) => void }) {
  const [open, setOpen] = createSignal(false);
  const [token, setToken] = createSignal<string | null>(null);
  const [err, setErr] = createSignal<string | null>(null);
  const now = createClock(CLOCK_TICK_MS);
  const me = useMe();
  const hook = () => p.iface.webhook!;
  const label = () => entityLabel({ name: p.iface.endpoint, label: p.iface.label });
  const base = () => `${typeof window === "undefined" ? "" : window.location.origin}/api/v1`;
  const url = () => base() + (token() ? `/webhooks/${token()}` : hook().path);
  const canReveal = createMemo(() => !!hook().secret && can(me.data, "secret", "reveal"));
  const refusedTitle = () => {
    const r = hook().refused;
    return r ? `The scheme refused ${r.count} post${r.count === 1 ? "" : "s"}, each answered 401. Latest: ${r.reason}, from ${r.source}, ${rel(r.at, now())}.` : "";
  };
  const reveal = async () => {
    setErr(null);
    try {
      setToken((await revealSecret(hook().secret!)).token ?? null);
    } catch {
      setErr("Could not reveal the token.");
    }
  };
  const copy = async () => {
    setErr(null);
    try {
      const t = (await copySecret(hook().secret!)).token;
      await navigator.clipboard.writeText(`${base()}/webhooks/${t}`);
    } catch {
      setErr("Could not copy the URL.");
    }
  };
  return (
    <div class="overflow-hidden">
      <div class="flex items-stretch">
        <button
          type="button"
          class="flex min-w-0 flex-1 items-center gap-3 px-3 py-2.5 text-left hover:bg-base-content/5"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open()}
        >
          <span class={`shrink-0 text-base-content/40 transition-transform ${open() ? "rotate-90" : ""}`}><ChevronRight size={14} /></span>
          <div class="flex w-52 shrink-0 flex-col gap-0.5">
            <span class="truncate text-sm">{label()}</span>
            <span class="truncate font-data text-[11px] text-base-content/50">{p.iface.transport} · hosted by the server</span>
          </div>
          <div class="min-w-0 flex-1 text-[12px] text-base-content/60">
            <Show when={p.iface.last_seen} fallback={<span>never heard</span>}>
              {(ts) => <span>last heard {rel(ts(), now())}</span>}
            </Show>
          </div>
        </button>
        <div class="flex shrink-0 items-center gap-2 pr-3">
          <InfoTip label="a push endpoint" text="A sender posts to this endpoint's URL on the server: nothing is dialed, so there is no ladder and no verdict. It reads last heard, stamped by every post that lands, and silence is never read as down." />
          <Show when={hook().refused}>
            <span class="badge badge-soft badge-error badge-sm shrink-0" title={refusedTitle()}>{hook().refused!.count} refused</span>
          </Show>
          <Show
            when={hook().signed}
            fallback={<span class="badge badge-soft badge-warning badge-sm shrink-0" title="Scheme none: nothing is checked, anyone holding the URL can post, and there is no replay protection">unsigned</span>}
          >
            <span class="badge badge-soft badge-success badge-sm shrink-0" title="The scheme that checks every post">{hook().scheme}</span>
          </Show>
          <span class="badge badge-soft badge-ghost badge-sm shrink-0">push</span>
        </div>
        <Show when={p.manageId && p.onManage}>
          <button
            type="button"
            class="flex shrink-0 items-center px-2.5 text-base-content/40 hover:bg-base-content/5 hover:text-base-content"
            title="Manage endpoint"
            aria-label={`Manage ${label()}`}
            onClick={() => p.onManage!(p.manageId!)}
          >
            <Sliders size={15} />
          </button>
        </Show>
      </div>
      <Show when={open()}>
        <div class="flex flex-wrap items-center gap-2 border-t border-base-300 bg-base-200/40 px-3 py-2 text-[12px]">
          <span class="text-base-content/50">posts to</span>
          <span class="min-w-0 truncate font-data text-base-content/80">{url()}</span>
          <span class="flex-1" />
          <Show when={canReveal()} fallback={<span class="text-[11px] text-base-content/45">Requires secret:reveal to show the token</span>}>
            <Button size="xs" icon={Eye} onClick={reveal} disabled={!!token()}>Reveal</Button>
            <Button size="xs" icon={Copy} onClick={copy}>Copy</Button>
          </Show>
        </div>
        <Show when={err()}>
          <div class="px-3 pb-2 text-[11px] text-error">{err()}</div>
        </Show>
      </Show>
    </div>
  );
}
