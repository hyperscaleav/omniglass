import { Show, createSignal, type JSX } from "solid-js";
import Button from "./Button";
import Drawer from "./Drawer";
import IssueCommandForm from "./IssueCommandForm";
import { Zap } from "./icons";
import { createBoundCommands } from "../lib/bound_commands";

// Send command (#2466): the step that resolves the alarm the operator is
// reading. It opens the same IssueCommandForm the component's Activity tab
// offers, with the component filled in, and is drawn only when the caller may
// issue a command AND a driver on one of the component's endpoints binds one
// (createBoundCommands, the same answer the panel's Issue button gives).
//
// The form lives in ONE host at the app shell, not in the row: an alarm list
// re-keys its rows on every poll, which would drop a form opened inside one
// (and what was typed in it). The row's button only asks the host to open.
type Request = { componentId: string; name: string; types: string[] };
const [request, setRequest] = createSignal<Request | undefined>();

export function useSendCommand(componentId: () => string) {
  const b = createBoundCommands(componentId);
  return { available: () => b.canIssue() && b.bound().length > 0, bound: b.bound };
}

export default function SendCommandButton(props: { componentId: string; name?: string; bound: string[] }): JSX.Element {
  const name = () => props.name ?? props.componentId;
  return (
    <Button
      square
      size="xs"
      icon={Zap}
      label="Send command"
      title={`Send a command to ${name()}: the same form as its Activity tab`}
      onClick={() => setRequest({ componentId: props.componentId, name: name(), types: props.bound })}
    />
  );
}

// SendCommandHost is the one drawer every Send command opens, mounted once in
// the shell. It rises above a blade's stack, since an alarm row often sits in one.
export function SendCommandHost(): JSX.Element {
  return (
    <Drawer open={!!request()} above onClose={() => setRequest(undefined)} title={`Issue a command to ${request()?.name ?? ""}`}>
      <Show when={request()} keyed>
        {(r) => <IssueCommandForm component={r.componentId} name={r.name} componentId={r.componentId} types={r.types} close={() => setRequest(undefined)} />}
      </Show>
    </Drawer>
  );
}
