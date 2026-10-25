import { For, Show, createSignal, type Component } from "solid-js";
import { Dynamic } from "solid-js/web";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import ConfirmDialog from "./ConfirmDialog";
import Eyebrow from "./Eyebrow";
import InfoTip from "./InfoTip";
import KVStacked from "./KVStacked";
import { RotateCcw, Shuffle, ShieldCheck, TriangleAlert } from "./icons";
import { useMe } from "../lib/auth";
import { describeError } from "../lib/format";
import { ENDPOINTS_KEY } from "../lib/endpoints";
import { REACHABILITY_KEY } from "../lib/reachability";
import {
  KEY_CHANGE_CAUSES,
  OFFERED_KEY_HINT,
  PINNED_KEY_HINT,
  RETRY_PENDING_KEY,
  keyNoun,
  retryWaiting,
  trustOfferedKey,
  trustRefusal,
  type KeyRefusal,
  type RetryPending,
} from "../lib/session";

// A refusal of a changed key, and the way to trust the new one (#1078). The
// node pins the key a component presents (an ssh host key; a tls
// certificate's public key next, #1035) and refuses a connection that offers
// another, publishing both keys with its gate. PinnedKeyNotice names the two
// keys, read off the gate as data and never out of the reason; TrustKeyButton
// declares the offered key as the new pin through :retry with host_key, behind
// endpoint:retry, after a confirm that teaches why a key changes. Generic over
// the class: a certificate refusal carrying both keys renders the same.

// PinnedKeyNotice is a key refusal's facts: the key pinned, the key offered.
// Side by side where its own width holds two fingerprints (a container query,
// so the Collection card and the narrower blade each get the layout that fits
// them), stacked otherwise.
export default function PinnedKeyNotice(props: { refusal: KeyRefusal }) {
  const noun = () => keyNoun(props.refusal.class);
  return (
    <div class="@container">
      <div data-testid="pinned-key-notice" class="grid gap-x-6 gap-y-2.5 pt-1 pb-0.5 @3xl:grid-cols-2">
        <KeyFact label={`Pinned ${noun()}`} hint={PINNED_KEY_HINT} value={props.refusal.pinned} testid="pinned-key" />
        <KeyFact label={`Offered ${noun()}`} hint={OFFERED_KEY_HINT} value={props.refusal.offered} testid="offered-key" />
      </div>
    </div>
  );
}

function KeyFact(props: { label: string; hint: string; value: string; testid: string }) {
  return (
    <KVStacked
      label={props.label}
      hint={props.hint}
      value={
        <span data-testid={props.testid} class="block break-all font-data text-[11.5px] leading-snug text-base-content/85">
          {props.value}
        </span>
      }
    />
  );
}

// The cause chips' glyphs, by position in KEY_CHANGE_CAUSES: the two routine
// causes wear the neutral chip, the one to fear the warning hue.
const CAUSE_LOOK: { icon: Component<{ size?: number }>; badge: string }[] = [
  { icon: RotateCcw, badge: "badge-ghost" },
  { icon: Shuffle, badge: "badge-ghost" },
  { icon: TriangleAlert, badge: "badge-soft badge-warning" },
];

// TrustKeyButton trusts the offered key, after a confirm. The confirm holds the
// refusal as it was when it opened, so what it sends is exactly what the
// operator read; a live read that moves the offered key while it is open
// disables it (the server would refuse the stale key anyway, 409). After a
// trust it waits the way Retry does (on the session, or for an endpoint
// holding none on the gate), sharing Retry's in-hand marker, so neither can
// stack a second attempt.
export function TrustKeyButton(props: {
  endpointID: string;
  // The owning component's uuid: the reachability read's cache key.
  component: string;
  // The endpoint as an operator names it, for the confirm's title.
  endpointLabel: string;
  refusal: KeyRefusal;
  // What a trust waits to see move: the held session's transition time, else
  // the gate's publish time (lib/session's retryMark).
  waitTS?: string;
  now: number;
}) {
  const me = useMe();
  const qc = useQueryClient();
  const [shown, setShown] = createSignal<KeyRefusal | null>(null);
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);
  const pending = useQuery(() => ({
    queryKey: RETRY_PENDING_KEY(props.endpointID),
    queryFn: (): RetryPending | null => null,
    enabled: false,
    staleTime: Infinity,
  }));
  const refusal = () => trustRefusal(me.data);
  const waiting = () => retryWaiting(pending.data, props.waitTS, props.now);
  const moved = () => {
    const s = shown();
    return !!s && s.offered !== props.refusal.offered;
  };

  function openConfirm() {
    if (refusal() || waiting()) return;
    setErr(null);
    setShown({ ...props.refusal });
  }

  async function trust() {
    const s = shown();
    if (!s || busy() || moved() || refusal()) return;
    const mark = props.waitTS ?? "";
    setBusy(true);
    setErr(null);
    try {
      await trustOfferedKey(props.endpointID, s.offered);
      qc.setQueryData<RetryPending>([...RETRY_PENDING_KEY(props.endpointID)], { ts: mark, at: Date.now() });
      setShown(null);
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
    <>
      <Show
        when={refusal()}
        fallback={
          <Button size="xs" icon={ShieldCheck} loading={busy() || waiting()} onClick={openConfirm} title={`Pin the ${keyNoun(props.refusal.class)} the component offers now`}>
            Trust this key
          </Button>
        }
      >
        {(why) => (
          <span class="tooltip tooltip-right" data-tip={why()} aria-label={why()} tabindex={0}>
            <Button size="xs" icon={ShieldCheck} disabled>Trust this key</Button>
          </span>
        )}
      </Show>
      <ConfirmDialog
        open={!!shown()}
        onClose={() => !busy() && setShown(null)}
        title={`Trust the new ${keyNoun(props.refusal.class)} for ${props.endpointLabel}?`}
        confirmLabel="Trust this key"
        confirmIcon={ShieldCheck}
        onConfirm={() => void trust()}
        busy={busy()}
        disabled={moved()}
        error={moved() ? `The component now offers a different key. Close this and read the new one before trusting it.` : err()}
      >
        <Show when={shown()}>
          {(s) => (
            <div class="flex flex-col gap-4">
              <PinnedKeyNotice refusal={s()} />
              <div class="flex flex-col gap-2">
                <Eyebrow
                  label="Why a key changes"
                  hint="Trust the offered key only when you know which of these happened. Trusting makes it the pin and the node redials onto it once; the audit log records both keys and who trusted."
                />
                <div class="flex flex-wrap gap-1.5">
                  <For each={KEY_CHANGE_CAUSES}>
                    {(cause, i) => (
                      <InfoTip text={cause.hint} label={cause.label} class={`badge badge-sm gap-1 ${CAUSE_LOOK[i()]?.badge ?? "badge-ghost"}`}>
                        <Dynamic component={CAUSE_LOOK[i()]?.icon ?? RotateCcw} size={12} />
                        {cause.label}
                      </InfoTip>
                    )}
                  </For>
                </div>
              </div>
            </div>
          )}
        </Show>
      </ConfirmDialog>
    </>
  );
}
