import { type JSX, Show } from "solid-js";
import { Trash, X } from "./icons";
import Button from "./Button";
import PanelFooter from "./PanelFooter";
import { type FormActions, type FormBinding } from "../lib/formactions";

// FormActionBar draws a form slot's binding (lib/formactions): the destructive
// action at the left, then Cancel, the quiet secondary and the submit at the
// right. The Drawer draws it pinned (through PanelFooter, the one pinned rail);
// a full-page create form draws it inline, the unpinned `border-t pt-4` row the
// page forms already use, for a body that binds rather than draws (#1329: the
// create page's "From a template" face). Either way the body writes no buttons.
export default function FormActionBar(props: { actions: FormActions; inline?: boolean }): JSX.Element {
  return (
    <Show when={props.actions.binding()}>
      {(b) => (
        <Show when={props.inline} fallback={<PanelFooter><Buttons b={b()} /></PanelFooter>}>
          <div class="flex items-center gap-2 border-t border-base-300 pt-4"><Buttons b={b()} /></div>
        </Show>
      )}
    </Show>
  );
}

function Buttons(props: { b: FormBinding }): JSX.Element {
  const b = () => props.b;
  return (
    <>
      <Show when={b().destructive?.()}>
        {(d) => (
          <Button intent="danger" icon={d().icon ?? Trash} onClick={() => d().onClick()} disabled={d().disabled || b().busy?.()}>
            {d().label}
          </Button>
        )}
      </Show>
      <div class="ml-auto flex items-center gap-2">
        <Show when={b().cancel}>
          {(c) => (
            <Button icon={X} onClick={() => c()()} disabled={b().busy?.()}>
              {b().cancelLabel ?? "Cancel"}
            </Button>
          )}
        </Show>
        <Show when={b().secondary?.()}>
          {(s) => (
            <Button icon={s().icon} onClick={() => s().onClick()} disabled={s().disabled || b().busy?.()}>
              {s().label}
            </Button>
          )}
        </Show>
        <Show when={b().submit}>
          {(submit) => (
            <Button intent="action" icon={b().submitIcon} onClick={() => submit()()} loading={b().busy?.()} disabled={b().disabled?.()}>
              {b().submitLabel}
            </Button>
          )}
        </Show>
      </div>
    </>
  );
}
