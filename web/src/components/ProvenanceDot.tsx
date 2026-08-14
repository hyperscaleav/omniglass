import { Tooltip } from "@kobalte/core/tooltip";
import { type JSX } from "solid-js";

// ProvenanceDot: the console's mark for a value that comes from somewhere that
// is NOT this row. One teal dot beside the field's label, present or absent,
// with nothing in between.
//
// The inversion is the point. A mark beside a value cannot usefully say "this
// one is mine", because a value that is plainly here already says so; the only
// statement worth making is that the value lives somewhere else. So the dot has
// exactly one state, and its absence is the other half of the vocabulary.
//
// It carries no encoding of DISTANCE, deliberately. A per-rung mark was built
// first and failed on real data twice over: its width was driven by how deep
// that corner of the estate happens to nest (8px at two rungs, 28px at six, the
// same object in the design's mind and a different one on screen), and three
// marks encoding the same chain never lined up, because each trailed a label of
// a different length. A dot has no width to spend and no position to compare.
//
// It sits beside the LABEL rather than beside the value because the label
// occupies the same position in both of a field's states. Beside the value
// there is no single right answer: trailing reads correctly on a fact and lands
// 380px from the placeholder inside a full-width input, where the console's own
// in-field actions live, and leading reads as a list bullet on a stack of
// facts. Beside the label, one treatment covers read and edit (FieldRow's label
// row and KVStacked's eyebrow are the same slot), so the mark does not move
// when the pencil is clicked.
//
// The hover names the source. The full CHAIN is a follow-on that needs the
// server to serve it: `inherited_<fact>_source` names the ancestor that
// supplied the value and nothing between, and climbing the type chain in
// TypeScript to fill the gap is what #695 deleted and #702 and #710 both
// refused to reinstate.
export default function ProvenanceDot(props: {
  // The field this mark belongs to, for the accessible name. A mark reading
  // only "inherited from mic" would leave a screen reader user to work out
  // which of three fields it was attached to.
  label: string;
  // The row the value came from, as the server attributes it.
  from: string;
}): JSX.Element {
  return (
    <Tooltip openDelay={150} closeDelay={100} placement="top" gutter={4}>
      <Tooltip.Trigger
        type="button"
        // The whole fact is in the accessible NAME, not only in the tooltip. A
        // hover has no keyboard and no touch equivalent, so a mark that states
        // itself only on hover is unreadable for anyone not using a mouse; the
        // name states it outright, and the trigger is a button, so it is a tab
        // stop that reveals the same words on focus.
        aria-label={`${props.label} is inherited from ${props.from}`}
        // Teal, because the mark is emphasis. `text-primary` is the console's
        // one teal for a small glyph and app.css darkens it on the light ground,
        // where the bright brand fill would be a 1.9:1 dot. The padding is the
        // hit area and the negative vertical margin gives the row its height
        // back, so a 6px dot is reachable without a 6px label row becoming an
        // 18px one.
        class="inline-flex shrink-0 items-center justify-center rounded-full p-1.5 -my-1.5 text-primary transition-colors hover:text-primary/70 focus:outline-none focus-visible:ring-1 focus-visible:ring-primary/60"
      >
        <span class="block h-[6px] w-[6px] rounded-full bg-current" />
      </Tooltip.Trigger>
      <Tooltip.Portal>
        {/*
          Portaled, so a blade's overflow cannot clip it, and reset to normal
          case and tracking, because the mark sits inside an eyebrow whose
          small-caps styling would otherwise be inherited by the tooltip's text.
        */}
        <Tooltip.Content class="z-100 max-w-56 rounded-box border border-base-300 bg-base-100 px-2.5 py-1.5 text-xs font-normal normal-case leading-snug tracking-normal text-base-content shadow-lg">
          Inherited from <span class="font-data">{props.from}</span>
        </Tooltip.Content>
      </Tooltip.Portal>
    </Tooltip>
  );
}
