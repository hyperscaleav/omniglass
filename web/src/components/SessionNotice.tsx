import { Show, createSignal, type Component } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import Eyebrow from "./Eyebrow";
import PinnedKeyNotice, { TrustKeyButton } from "./PinnedKeyNotice";
import { RotateCcw, Sliders } from "./icons";
import { useMe } from "../lib/auth";
import { entityLabel } from "../lib/entities";
import { describeError } from "../lib/format";
import { ENDPOINTS_KEY } from "../lib/endpoints";
import { REACHABILITY_KEY, type ReachEndpoint } from "../lib/reachability";
import {
  RETRY_PENDING_KEY,
  endpointNotice,
  keyChangedHint,
  keyNoun,
  keyRefusal,
  retryEndpoint,
  retryMark,
  retryRefusal,
  retryWaiting,
  type RetryPending,
} from "../lib/session";

// SessionNotice says why an endpoint is refused or its held session down
// (#1059): for a refusal, the reason (a rejected session's own words, else the
// gate's: its class and the component's own text, scrubbed of anything the
// node sent) and the two ways out. A refusal shows whatever met it: the gate
// holds whether or not a session is held, so an endpoint refused by its
// reachability probe alone (a tls certificate, a udp terminal reply, an snmp
// authorizationError, an ssh probe's credential) reads the same as a rejected
// session (#1110). Retry moves the endpoint's retry counter (POST
// /endpoints/{id}:retry, audited), so the node tries once more; it greys with
// its reason for a caller without endpoint:retry (a hint only, the server is
// the authority and says why it refused). The fix path is the host's: the
// endpoint blade begins its own edit, where the credential is one of the
// inputs, and a list opens that blade. A down session offers no retry: the
// node is already redialing on its own.
//
// After a retry the notice waits on what the retry moves (lib/session's
// retryMark): the read it renders is invalidated (and polled by the
// component's view), and the button holds its spinner until the session's
// transition time moves (a fresh establishing, up, or rejected again) or, for
// an endpoint holding no session, the gate's publish time does (a refusal met
// again), or RETRY_WAIT_MS passes, so a second click cannot stack attempts
// while the node has not picked up the first. The retry in hand lives in the
// query cache under the endpoint (lib/session), so every notice for the
// endpoint holds it, not only the one clicked.
//
// A refusal of a changed key (#1078) names both keys in place of the reason,
// read off the gate as data (PinnedKeyNotice), and leads its ways out with
// Trust this key, which declares the offered key as the new pin after a
// confirm. The node's reason names the same two keys in prose, so it is not
// repeated beside them.

export default function SessionNotice(props: {
  iface: ReachEndpoint;
  // The owning component's uuid: the reachability read's cache key.
  component: string;
  now: number;
  onFix?: () => void;
  fixLabel?: string;
  // The fix's glyph: the endpoint's Manage sliders by default (it opens the
  // endpoint), a pencil where the fix is an edit in place.
  fixIcon?: Component<{ size?: number }>;
}) {
  const me = useMe();
  const qc = useQueryClient();
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);
  // Read only, never fetched: a retry sets it (below), every notice for the
  // endpoint observes it.
  const pending = useQuery(() => ({
    queryKey: RETRY_PENDING_KEY(props.iface.id ?? ""),
    queryFn: (): RetryPending | null => null,
    enabled: false,
    staleTime: Infinity,
  }));
  const notice = () => endpointNotice(props.iface, props.now);
  const rejected = () => notice()?.kind === "rejected";
  const keys = () => (rejected() ? keyRefusal(props.iface) : null);
  const refusal = () => retryRefusal(me.data);
  const waiting = () => retryWaiting(pending.data, retryMark(props.iface), props.now);

  async function retry() {
    const id = props.iface.id;
    if (!id || refusal()) return;
    // What the retry waits on, as it stood when asked for: a read that lands
    // while the call is in flight must not become the mark it waits past.
    const mark = retryMark(props.iface);
    setBusy(true);
    setErr(null);
    try {
      await retryEndpoint(id);
      qc.setQueryData<RetryPending>([...RETRY_PENDING_KEY(id)], { ts: mark, at: Date.now() });
      await Promise.all([
        qc.invalidateQueries({ queryKey: [...REACHABILITY_KEY(props.component)] }),
        qc.invalidateQueries({ queryKey: [...ENDPOINTS_KEY] }),
      ]);
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Show when={notice()}>
      <div
        data-testid="session-notice"
        data-state={notice()?.kind}
        class="flex flex-col gap-1.5 rounded-box border px-3 py-2 text-xs"
        classList={{ "border-warning/40 bg-warning/5": rejected(), "border-error/40 bg-error/5": !rejected() }}
      >
        <Show
          when={keys()}
          fallback={
            <>
              <Eyebrow
                label={rejected() ? "Why it was refused" : "Why it is down"}
                hint={
                  rejected()
                    ? "The component's own words, after the class the driver or the transport gave them. Whatever met the refusal (the reachability probe, a poll, a held session), nothing at or above the refused rung is tried again until something the endpoint connects with changes (the credential, the address, the port) or someone retries it."
                    : "The node's account of the last attempt. It keeps redialing at a slow pace, so a component that returns comes back up on its own."
                }
              />
              <p data-testid="session-reason" class="break-words font-data text-[11.5px] text-base-content/80">{notice()?.reason}</p>
            </>
          }
        >
          {(k) => (
            <>
              <Eyebrow label={`The ${keyNoun(k().class)} changed`} hint={keyChangedHint(k().class)} />
              <PinnedKeyNotice refusal={k()} />
            </>
          )}
        </Show>
        <Show when={rejected() && props.iface.id}>
          <div class="flex flex-wrap items-center gap-2">
            <Show when={keys()}>
              {(k) => (
                <TrustKeyButton
                  endpointID={props.iface.id!}
                  component={props.component}
                  endpointLabel={entityLabel({ name: props.iface.endpoint, label: props.iface.label })}
                  refusal={k()}
                  waitTS={retryMark(props.iface)}
                  now={props.now}
                />
              )}
            </Show>
            <Show
              when={refusal()}
              fallback={
                <Button size="xs" icon={RotateCcw} loading={busy() || waiting()} onClick={() => void retry()} title="Try once more, with the endpoint as it is now">
                  Retry
                </Button>
              }
            >
              {(why) => (
                <span class="tooltip tooltip-right" data-tip={why()} aria-label={why()} tabindex={0}>
                  <Button size="xs" icon={RotateCcw} disabled>Retry</Button>
                </span>
              )}
            </Show>
            <Show when={props.onFix}>
              <Button size="xs" icon={props.fixIcon ?? Sliders} onClick={() => props.onFix!()}>{props.fixLabel ?? "Open endpoint"}</Button>
            </Show>
          </div>
        </Show>
        <Show when={err()}>
          <div role="alert" class="text-[11.5px] text-error">{err()}</div>
        </Show>
      </div>
    </Show>
  );
}
