import { type JSX } from "solid-js";

// PanelFooter is the console's one action rail: the pinned bar at the bottom of a
// panel that holds its entity actions. It exists so the blade's rail and the
// Drawer's rail cannot drift apart in border, background, padding, or height, which
// is exactly what happened while each shell wrote its own.
//
// It owns the rail, not the buttons. A shell composes what goes inside (the blade
// puts a destructive action left and its Edit / Save cluster right; a Drawer
// right-aligns submit and cancel), because those vocabularies genuinely differ.
// `dimmed` is the covered-blade state: visible but inert. `notice` rides above
// the buttons inside the rail (a failed save's reason), so it sits beside them.
export default function PanelFooter(props: { dimmed?: boolean; notice?: JSX.Element; children: JSX.Element }): JSX.Element {
  return (
    <footer
      class="flex flex-none flex-col gap-2 border-t border-base-300 bg-base-100 px-5 py-3"
      classList={{ "pointer-events-none opacity-55": !!props.dimmed }}
    >
      {props.notice}
      <div class="flex items-center gap-2">{props.children}</div>
    </footer>
  );
}
