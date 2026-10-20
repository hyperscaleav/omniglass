import { createMemo, type JSX } from "solid-js";
import { entityLabel, labelIsName, type Labelled } from "../lib/entities";

// BladeTitle is the console's one blade heading: the label of the row the
// operator clicked, falling back to the identifier when the row carries no
// label (or has not resolved yet), with the identifier rendered in the
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
//
// The accessor itself is read ONCE, in the body, and only its value is tracked
// (#953). Written inline, `row={useXRow(id)}` compiles to a getter that calls
// the hook on every read of `p.row`; read inside the memo, each list refetch
// re-ran the memo, minted a fresh query observer, and that observer refetched
// at once, a request storm on the list while the blade was open. Callers
// create the accessor in their own component body (`const row = useXRow(id)`)
// and pass the variable; a guard test forbids the inline hook call, and this
// one read keeps the primitive safe even if one slips past it.
export default function BladeTitle(p: {
  // The row this blade is about, as an accessor so the heading tracks it.
  // Create it once in the caller's body; never call a hook in the prop.
  row: () => Labelled | undefined;
  // What to show before the row resolves: the id or name the blade was opened
  // with. Always an identifier, so it renders in the data face.
  fallback: string;
}): JSX.Element {
  const source = p.row;
  const row = createMemo(() => source());
  // The face follows the LABEL, through the primitive, not a second reading of
  // label here. The two agreed while every label was operator-typed and
  // would have parted the moment one of them learned about the pen (#683).
  const dataFace = () => {
    const r = row();
    return r ? labelIsName(r) : true; // the fallback is always an identifier
  };
  return <span classList={{ "font-data": dataFace() }}>{row() ? entityLabel(row()!) : p.fallback}</span>;
}
