import { Show, type JSX } from "solid-js";
import ProvenanceDot from "./ProvenanceDot";
import { EMPTY_VALUE } from "./BladeField";
import type { InheritedFact } from "../lib/catalog";

// InheritedCell is a type registry LIST cell for a fact that INHERITS: the stem,
// abbrev and icon a component_type or system_type takes from the nearest
// ancestor that states one when it states none of its own. It is
// InheritedField's list-side sibling, over the same served answers, asking the
// same one predicate.
//
// It exists because the list and the blade disagreed about the same fact two
// clicks apart (#743). #716 and #742 made the blade show what a row inherits and
// name where it came from; the Stem and Abbrev cells went on printing an em dash
// on exactly those rows, so the console asserted a value was absent while its
// own detail view showed it. Nothing is fetched for this: `inherited_*` and
// `inherited_*_source` already ride the registry listing, which is also the
// blade's read, so both surfaces are rendering one row from one query.
//
// The MARK is the whole distinction, deliberately, and this is where a list cell
// parts company with a blade field. The blade tells an inherited value apart by
// muting it beside a stated value that is not muted; a table column has no such
// pair to sit beside, because EVERY value in these columns is muted already
// (`text-base-content/60`, the console's secondary-column treatment). So dimming
// an inherited one further would mean either brightening every stated value,
// which is a change to the rows that are not the subject, or pushing the
// inherited ones down to `/40`, which is the treatment this console already
// gives an ABSENT value (EMPTY_VALUE renders there in BladeField's read state).
// Borrowing it would make "comes from elsewhere" look like "nothing here", which
// is the exact confusion #743 is about. So the dot carries it alone: present or
// absent, one teal, nothing in between, which is the vocabulary #742 settled and
// ProvenanceDot holds.
//
// The mark TRAILS the value here rather than sitting beside the label, because a
// table's label is in the header, one per column instead of one per row: there
// is no per-row label slot for it to share. Leading it would read as a bullet
// down the column rather than as a mark on a value.
export default function InheritedCell(props: {
  // The column, for the mark's accessible name ("Stem is inherited from mic").
  // The mark states the whole fact in its name rather than only on hover, so a
  // column of dots is readable without a mouse.
  label: string;
  // What this row states for itself. Empty (or absent) is the whole of "this
  // node declares no fact of its own", the state the inheritance walk resumes
  // from, and the same predicate InheritedField asks of an empty box.
  own: string | undefined;
  // What the server says this row would take if it stated none, and the ancestor
  // that supplied it. Both halves are absent together.
  inherited: InheritedFact;
  // What to show when the row states nothing, for the one fact the server
  // answers twice: the Icon column renders `resolved_icon` (what the row SHOWS)
  // rather than `inherited_icon` (what an emptied box would take), which are the
  // same string on a row stating no icon and two different questions everywhere
  // else. Defaults to the inherited value, which is the right answer for stem
  // and abbrev, where the server resolves nothing else.
  shown?: string;
  // A glyph rendered ahead of the text (the Icon column's own render).
  leading?: JSX.Element;
}): JSX.Element {
  const shown = () => props.shown ?? props.inherited.value;
  const text = () => props.own || shown();
  // ONE predicate, the same one InheritedField asks: this value comes from
  // elsewhere when the row states nothing and something above it does. A row
  // that states nothing with nothing above it is genuinely empty, and keeps the
  // em dash it always had, because deleting that outright would be this defect
  // pointed the other way, a cell claiming a value that does not exist.
  const inheriting = () => !props.own && props.inherited.value !== "";
  return (
    <span class="flex items-center gap-1.5">
      {props.leading}
      <span class="font-data text-xs text-base-content/60">{text() || EMPTY_VALUE}</span>
      <Show when={inheriting()}>
        <ProvenanceDot label={props.label} from={props.inherited.from} />
      </Show>
    </span>
  );
}
