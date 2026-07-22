import { type Component, type Context, createContext, createSignal, onCleanup, useContext } from "solid-js";

// The form action slot: a panel's body registers what its action bar DOES, and the
// shell draws it. This is the create/submit sibling of the blade's edit slot (see
// lib/blades): BladeStack owns the blade's footer, Drawer owns the Drawer's, and in
// both cases the body writes no footer markup at all. That is the point. The rail
// used to be an opt-in helper each form wrapped its own buttons in, which meant a
// form could simply forget it, and two of them did for months while the helper was
// copied into six new pages around them. A slot cannot be forgotten: bind or get no
// bar.
//
// Deliberately smaller than BladeEdit. There is no read -> Edit -> Save cycle here
// (a create form is always "editing"), and no secondary or destructive slot, because
// no create form has ever wanted one. Add them when a caller does, not before.

export type FormBinding = {
  submitLabel: string;
  // The icon COMPONENT (Button sizes it), not an element. No default: an explicit
  // Plus on a create and an explicit Check on a confirm beats a magic one that is
  // silently wrong on the surface that is not creating anything.
  submitIcon?: Component<{ size?: number }>;
  submit: () => void;
  // In flight: the submit button spins and both buttons disable.
  busy?: () => boolean;
  // The form is incomplete or invalid, so submit is not offered.
  disabled?: () => boolean;
  // Present -> a Cancel button renders beside submit. Absent -> the header's close
  // is the only way out, which is right for a one-button create.
  cancel?: () => void;
  cancelLabel?: string;
};

export type FormActions = {
  binding: () => FormBinding | undefined;
  bind: (b: FormBinding) => void;
};

export function createFormActions(): FormActions {
  const [binding, setBinding] = createSignal<FormBinding | undefined>();
  return {
    binding,
    bind: (b) => {
      setBinding(() => b);
      // Clear on unmount, but ONLY our own binding. A body whose branches each bind
      // (Profile's token drawer swaps a create form for a reveal panel) can dispose
      // the outgoing branch after the incoming one has already bound; an unguarded
      // cleanup would blank the bar that just replaced it.
      onCleanup(() => setBinding((cur) => (cur === b ? undefined : cur)));
    },
  };
}

export const FormActionsContext: Context<FormActions | undefined> = createContext<FormActions>();

export function useFormActions(): FormActions {
  const a = useContext(FormActionsContext);
  if (!a) throw new Error("useFormActions called outside a FormActionsContext provider (is the body inside a Drawer?)");
  return a;
}
