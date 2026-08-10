import { Show } from "solid-js";
import { entityLabel, hasDisplayName, labelGenerated, labelIsName, type Labelled } from "../lib/entities";

// How an entity's identity reads in a list, in one place.
//
// The platform's identity triad is `id` (a uuid, immutable), `name` (the
// renameable identifier an operator types, `hq-boardroom-dsp`), and
// `display_name` (an optional friendly string a human reads, "HQ Boardroom
// DSP"). Two of those are operator-facing, and the rule between them is one
// sentence: the display name is the primary line, the name sits beneath it, and
// the name is suppressed when the two are the same thing.
//
// That rule was previously written sixteen times, in four mutually incompatible
// idioms, one per FlatList page:
//
//   - two columns, one per field                   (Vendors, Drivers,
//     Products, Standards, Types)
//   - "Key" and "Label" columns                    (Properties, EventTypes,
//     CommandTypes, Tags)
//   - one column, display name plus inline muted name (Groups, Roles, Users, Nodes)
//   - the name alone                               (Secrets, Variables)
//
// Four idioms is why the same estate read four different ways depending on which
// page an operator was on, and why the column header for the same fact was "Name"
// here and "Key" there. Those retired words are named here because this comment is
// the history; the guard forbids a second word for the identifier everywhere else.
//
// The two-line treatment matches what TreeList already renders, so a tree and a
// flat list of the same entity look like the same product.

// Why the name is always shown rather than revealed on hover: hover does not
// exist on touch, is not discoverable, and cannot be selected to copy. Copying it
// into a CLI invocation is the entire point of showing it.
//
// Why the name is NOT re-cased or prettified into a display name when the display
// name is absent: this domain is acronyms (DSP, HDMI, NVX, PTZ, UC, AVoIP), so any
// mechanical casing mangles them, and it would make an ABSENT display name look
// like a typo rather than an absence. When the display name IS the name it renders
// once, in the data face, which marks it as an identifier rather than a friendly
// string somebody chose.
//
// The pen split the one flag this cell used to read into three states (#683). A
// label a RULE rendered (#682) differs from the name exactly as an operator's
// does, so the old "the label differs from the name" would have put a second
// identifier line under every row in a 15,000-component estate. The three states,
// and what each renders:
//
//   no label            the name, once, in the data face (it IS an identifier)
//   operator's label    the label in the prose face, the name beneath it
//   platform's label    the label in the prose face, marked, no second line
//
// The face follows the LABEL, not the second line: a rendered label is prose, so
// it must not drop into the data face merely because no identifier follows it.
export function IdentityCell(props: { entity: Labelled; weight?: number }) {
  const label = () => entityLabel(props.entity);
  const showName = () => hasDisplayName(props.entity);
  const generated = () => labelGenerated(props.entity);
  // The label IS the name, so it reads as an identifier and takes the data face.
  const isIdentifier = () => labelIsName(props.entity);
  return (
    <span class="flex min-w-0 flex-col gap-0.5 py-0.5">
      <span class="flex min-w-0 items-baseline gap-1.5">
        <span
          class="truncate"
          classList={{ "font-data text-[13px]": isIdentifier() }}
          style={{ "font-weight": props.weight ?? 500 }}
        >
          {label()}
        </span>
        {/* The label pen's tracking chip, the same vocabulary the name's pen
            already wears on the component blade. Typing a label claims it and
            clearing the field hands it back, so this is the mark that says which
            rows a rule edit would rewrite. */}
        <Show when={generated()}>
          <span
            class="badge badge-ghost badge-xs shrink-0"
            title="The platform generated this label from a label rule. Typing one of your own claims it; clearing the field hands it back."
          >
            Generated
          </span>
        </Show>
      </span>
      <Show when={showName()}>
        <span class="truncate font-data text-[11px] text-base-content/40">{props.entity.name}</span>
      </Show>
    </span>
  );
}

// identityColumn is the FlatList column every page uses instead of hand-rolling a
// name column. One header word ("Name"), one cell, one sort key, everywhere. The
// header is deliberately not overridable: a page that could rename it is a page
// that can invent a second word for the identifier, which is the drift this
// primitive exists to end.
//
// It sorts on the display name rather than the name, because the display name is
// what the operator is reading down; sorting on a hidden-when-equal name would make
// the order look arbitrary on any page where most rows carry a display name.
export function identityColumn<T extends Labelled>(opts?: { weight?: (row: T) => number }) {
  return {
    key: "name",
    label: "Name",
    sortVal: (row: T) => entityLabel(row),
    cell: (row: T) => <IdentityCell entity={row} weight={opts?.weight?.(row)} />,
  };
}
