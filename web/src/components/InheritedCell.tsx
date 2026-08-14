import { type JSX } from "solid-js";
import { EMPTY_VALUE } from "./BladeField";

// InheritedCell is a type registry LIST cell for a fact that INHERITS: the stem,
// abbrev and icon a component_type or system_type takes from the nearest
// ancestor that states one when it states none of its own.
//
// It exists because the list and the blade disagreed about the same fact two
// clicks apart (#743). #716 and #742 made the blade show what a row inherits;
// the Stem and Abbrev cells went on printing an em dash on exactly those rows,
// so the console asserted a value was absent while its own detail view showed
// it. Nothing is fetched for this: `inherited_*` already rides the registry
// listing, which is also the blade's read, so both surfaces render one row from
// one query.
//
// It shows the value UNDIFFERENTIATED, and that is the ruling rather than an
// omission. A table is for scanning values; the blade is where a value's origin
// is explained, and the provenance mark is a blade and detail affordance only.
// So a cell answers "what is this row's stem" and says nothing about where the
// answer came from, which is the treatment the Icon column has had since #695
// and is now the house rule for a table rather than the odd column out.
//
// What is left is one fallback chain and one treatment, in one place instead of
// at six cells across two registries: the row's own value, else what the server
// resolved for it, else the em dash. The em dash keeps its one meaning, nothing
// here and nothing above, because deleting it outright would be this defect
// pointed the other way, a cell asserting a value that does not exist.
export default function InheritedCell(props: {
  // What this row states for itself. Empty (or absent) is the whole of "this
  // node declares no fact of its own", the state the inheritance walk resumes
  // from, and the same predicate an empty box means in the blade.
  own: string | undefined;
  // What the server resolved for this row: the value it takes when it states
  // none. That is `inherited_<fact>` for stem and abbrev, and `resolved_icon`
  // for the Icon column, which is the same answer on a row stating no icon and a
  // different question everywhere else (what a row SHOWS, versus what an emptied
  // box would take). Empty means nothing above states this fact either.
  resolved: string;
  // A glyph rendered ahead of the text (the Icon column's own render).
  leading?: JSX.Element;
}): JSX.Element {
  return (
    <span class="flex items-center gap-1.5">
      {props.leading}
      <span class="font-data text-xs text-base-content/60">{props.own || props.resolved || EMPTY_VALUE}</span>
    </span>
  );
}
