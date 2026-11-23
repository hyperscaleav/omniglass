import { createSignal, type JSX } from "solid-js";
import Button from "./Button";
import Drawer from "./Drawer";
import IssueCommandForm from "./IssueCommandForm";
import { Zap } from "./icons";
import { createBoundCommands } from "../lib/bound_commands";

// SendCommandButton (#2466): "Send command" on an alarm's row, the step that
// resolves the alarm the operator is reading. It opens the same
// IssueCommandForm the component's Activity tab offers, with the component
// filled in, and is drawn only when the caller may issue a command AND a driver
// on one of the component's endpoints binds one (createBoundCommands, the same
// answer the panel's Issue button gives).
export function useSendCommand(componentId: () => string) {
  const b = createBoundCommands(componentId);
  return { available: () => b.canIssue() && b.bound().length > 0, bound: b.bound };
}

export default function SendCommandButton(props: { componentId: string; name?: string; bound: string[] }): JSX.Element {
  const [open, setOpen] = createSignal(false);
  const name = () => props.name ?? props.componentId;
  return (
    <>
      <Button square size="xs" icon={Zap} label="Send command" title={`Send a command to ${name()}: the same form as its Activity tab`} onClick={() => setOpen(true)} />
      <Drawer open={open()} onClose={() => setOpen(false)} title={`Issue a command to ${name()}`}>
        <IssueCommandForm component={props.componentId} name={name()} componentId={props.componentId} types={props.bound} close={() => setOpen(false)} />
      </Drawer>
    </>
  );
}
