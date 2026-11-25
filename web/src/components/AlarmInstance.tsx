import { Show, type JSX } from "solid-js";

// AlarmInstance names what a rule's alarm is about within its component, read
// after its message: the endpoint a reachability alarm names ("Reachability
// lost on http"). Two alarms of one rule on one component read the same
// message and differ only here, so a list that shows both tells them apart.
// Nothing for an alarm without one (a hand raise, a rule over one series).
export default function AlarmInstance(props: { instance?: string | null }): JSX.Element {
  return (
    <Show when={props.instance}>
      {(i) => (
        <>
          {" "}
          <span data-testid="alarm-instance" class="font-data text-base-content/60">on {i()}</span>
        </>
      )}
    </Show>
  );
}
