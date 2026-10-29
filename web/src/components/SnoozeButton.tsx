import { For, Show, createSignal, type JSX } from "solid-js";
import { Popover } from "@kobalte/core/popover";
import { useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import { Moon } from "./icons";
import { ALARM_TIMELINE_PREFIX, COMPONENT_ALARMS_PREFIX, SNOOZE_PRESETS, snoozeAlarm, snoozedUntilWords, unsnoozeAlarm } from "../lib/alarms";
import { FLEET_VIEW_KEY } from "../lib/fleet";
import { can, useMe } from "../lib/auth";
import { describeError, fmtTime } from "../lib/format";

type Snoozable = { id: string; snoozed_until?: string; snoozed_by?: string };

// Which control's duration menu is open, and the last error of each, held
// outside the row: an alarm list that polls hands its rows new objects, so a
// row (and any state it owned) is rebuilt every few seconds, and a menu or an
// error held in the row would vanish under the operator. The key is the
// surface (a list-owned id from createUniqueId, which outlives its rows) plus
// the alarm, since one alarm is often on screen twice at once (the blade's
// Why strip and its alarms panel), and two menus opened together would
// dismiss each other.
const [openMenu, setOpenMenu] = createSignal<string | null>(null);
const [errors, setErrors] = createSignal<Record<string, string>>({});
const setError = (key: string, msg: string | null) =>
  setErrors((prev) => {
    const next = { ...prev };
    if (msg) next[key] = msg;
    else delete next[key];
    return next;
  });

// What a snooze is, against the three acts it is mistaken for: the tooltip on
// the control, so the difference is taught where the choice is made.
export const SNOOZE_TIP =
  "Silence this condition's notifications for a while: nobody is paged, mailed or told in their inbox about it raising or clearing, and what it suppresses stays on the timeline, reading snoozed. " +
  "Unlike acknowledging, it records nothing about who has seen it. Unlike opting out, it silences everyone. Unlike pausing a rule, it silences this condition alone. " +
  "If it is still raised when the snooze ends, people are told again.";

// SnoozedBadge (#1318): the snooze on the alarm's condition, for every reader
// (a viewer sees it and holds no control). It names who set it and the exact
// deadline in its tooltip.
export function SnoozedBadge(props: { alarm: Snoozable }): JSX.Element {
  return (
    <Show when={props.alarm.snoozed_until}>
      {(until) => (
        <span
          class="badge badge-snoozed badge-xs shrink-0"
          title={`Its notifications are recorded, not sent, until ${fmtTime(until())}${props.alarm.snoozed_by ? `, snoozed by ${props.alarm.snoozed_by}` : ""}`}
        >
          {snoozedUntilWords(until())}
        </span>
      )}
    </Show>
  );
}

// SnoozeButton (#1318) sits beside AcknowledgeButton wherever an alarm offers
// acknowledgement. A snooze belongs to the alarm's CONDITION (its component
// and dedup key), so snoozing one alarm silences every alarm of that
// condition, the next re-raise included. It is gated on its own permission,
// alarm:snooze, never on the component's update (ADR-1318), and renders
// nothing for a caller who may not. Unsnoozed, it opens a menu of durations;
// snoozed, it ends the snooze early.
export default function SnoozeButton(props: { component: string; alarm: Snoozable; surface: string }): JSX.Element {
  const me = useMe();
  return <SnoozeControl component={props.component} alarm={props.alarm} surface={props.surface} allowed={can(me.data, "alarm", "snooze")} />;
}

// SnoozeControl is the control itself, for a surface that already knows the
// caller's permissions (the alarms panel takes them as props, as it does
// canAcknowledge).
export function SnoozeControl(props: { component: string; alarm: Snoozable; surface: string; allowed: boolean }): JSX.Element {
  const qc = useQueryClient();
  const [busy, setBusy] = createSignal(false);
  const key = () => `${props.surface}:${props.alarm.id}`;
  const err = () => errors()[key()] ?? null;
  const run = async (write: () => Promise<unknown>) => {
    setBusy(true);
    setError(key(), null);
    setOpenMenu(null);
    try {
      await write();
      await Promise.all([
        qc.invalidateQueries({ queryKey: [...COMPONENT_ALARMS_PREFIX] }),
        qc.invalidateQueries({ queryKey: ["system-alarms"] }),
        qc.invalidateQueries({ queryKey: [...FLEET_VIEW_KEY] }),
        qc.invalidateQueries({ queryKey: [...ALARM_TIMELINE_PREFIX] }),
      ]);
    } catch (e) {
      setError(key(), describeError(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Show when={props.allowed}>
      <Show
        when={!props.alarm.snoozed_until}
        fallback={
          <Button size="xs" intent="quiet" loading={busy()} title={err() ?? "End the snooze now. If the condition is still raised, people are told again."}
            onClick={() => void run(() => unsnoozeAlarm(props.component, props.alarm.id))}>
            Unsnooze
          </Button>
        }
      >
        <Popover placement="bottom-end" gutter={4} open={openMenu() === key()} onOpenChange={(o) => {
            if (o) setOpenMenu(key());
            else if (openMenu() === key()) setOpenMenu(null);
          }}>
          <Popover.Trigger class="btn btn-quiet btn-xs gap-1.5 self-center" title={err() ?? SNOOZE_TIP} disabled={busy()}>
            <Moon size={14} />
            Snooze
          </Popover.Trigger>
          <Popover.Portal>
            <Popover.Content class="menu z-50 w-44 rounded-box border border-base-300 bg-base-100 p-1.5 shadow-2xl focus:outline-none" aria-label="Snooze for">
              <p class="menu-title px-2 pb-1 text-[10.5px]">Silence this condition for</p>
              <ul class="flex flex-col">
                <For each={SNOOZE_PRESETS}>
                  {(p) => (
                    <li>
                      <button type="button" class="px-2 py-1.5 text-left text-[12px]"
                        onClick={() => void run(() => snoozeAlarm(props.component, props.alarm.id, p.duration))}>
                        {p.label}
                      </button>
                    </li>
                  )}
                </For>
              </ul>
            </Popover.Content>
          </Popover.Portal>
        </Popover>
      </Show>
      <Show when={err()}>
        <span role="alert" class="text-[11px] text-error">{err()}</span>
      </Show>
    </Show>
  );
}
