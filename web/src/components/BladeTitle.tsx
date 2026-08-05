import { createMemo, type JSX } from "solid-js";
import { entityLabel, type Labelled } from "../lib/entities";

// BladeTitle is the console's one blade heading: the display name of the row the
// operator clicked, falling back to the identifier when the row carries no
// display name (or has not resolved yet), with the identifier rendered in the
// data face because it is an identifier.
//
// It exists for the same reason BladeField does. Eight pages wrote this rule out
// by hand and all eight wrote it with the same bug: they read the row accessor
// ONCE, in the component body.
//
//   const r = row();                                  // <- outside any tracking scope
//   return <span>{r ? entityLabel(r) : p.id}</span>;
//
// A Solid component body runs once, so a read there subscribes to nothing. The
// refetch after a Save updated the row, the list, and the blade body, and the
// heading kept the old words until the blade was closed and reopened (#579). The
// fix is one character of placement, the accessor read inside the JSX, which is
// exactly the kind of rule that survives in a component and does not survive
// being retyped eight times.
export default function BladeTitle(p: {
  // The row this blade is about, as an accessor so the heading tracks it.
  row: () => Labelled | undefined;
  // What to show before the row resolves: the id or name the blade was opened
  // with. Always an identifier, so it renders in the data face.
  fallback: string;
}): JSX.Element {
  const row = createMemo(() => p.row());
  return (
    <span classList={{ "font-data": !row()?.display_name?.trim() }}>
      {row() ? entityLabel(row()!) : p.fallback}
    </span>
  );
}
