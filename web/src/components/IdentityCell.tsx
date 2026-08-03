import { Show } from "solid-js";
import { entityLabel, hasDisplayName, type Labelled } from "../lib/entities";

// How an entity's identity reads in a list, in one place.
//
// Every entity carries two identities an operator cares about: a **segment**
// (`name`, the kebab token the API and CLI address it by) and an optional
// **label** (`display_name`). The rule is one sentence: the label is the primary
// line, the segment sits beneath it, and the segment is suppressed when the two
// are the same thing.
//
// That rule was previously written sixteen times, in four mutually incompatible
// idioms, one per FlatList page:
//
//   - separate "Name" and "Display name" columns  (Vendors, Drivers, Capabilities,
//     Products, Standards, Types)
//   - "Key" and "Label" columns                    (Properties, EventTypes,
//     CommandTypes, Tags)
//   - one "Name" column, label plus inline muted segment (Groups, Roles, Users, Nodes)
//   - the segment alone                            (Secrets, Variables)
//
// Four idioms is why the same estate reads four different ways depending on which
// page an operator is on, and why the column header for the same fact is "Name"
// here, "Key" there, and "Display name" somewhere else.
//
// The two-line treatment matches what TreeList already renders, so a tree and a
// flat list of the same entity look like the same product.

// Why the segment is always shown rather than revealed on hover: hover does not
// exist on touch, is not discoverable, and cannot be selected to copy. Copying it
// into a CLI invocation is the entire point of showing it.
//
// Why the segment is NOT re-cased or prettified into a label when a label is
// absent: this domain is acronyms (DSP, HDMI, NVX, PTZ, UC, AVoIP), so any
// mechanical casing mangles them, and it would make an ABSENT label look like a
// typo rather than an absence. When the label IS the segment it renders once, in
// the data face, which marks it as an identifier rather than a name somebody chose.
export function IdentityCell(props: { entity: Labelled; weight?: number }) {
  const label = () => entityLabel(props.entity);
  const showSegment = () => hasDisplayName(props.entity);
  return (
    <span class="flex min-w-0 flex-col gap-0.5 py-0.5">
      <span
        class="truncate"
        classList={{ "font-data text-[13px]": !showSegment() }}
        style={{ "font-weight": props.weight ?? 500 }}
      >
        {label()}
      </span>
      <Show when={showSegment()}>
        <span class="truncate font-data text-[11px] text-base-content/40">{props.entity.name}</span>
      </Show>
    </span>
  );
}

// identityColumn is the FlatList column every page uses instead of hand-rolling a
// name column. One header word ("Name"), one cell, one sort key, everywhere.
//
// It sorts on the label rather than the segment, because the label is what the
// operator is reading down; sorting on a hidden-when-equal segment would make the
// order look arbitrary on any page where most rows carry labels.
export function identityColumn<T extends Labelled>(opts?: { label?: string; weight?: (row: T) => number }) {
  return {
    key: "name",
    label: opts?.label ?? "Name",
    sortVal: (row: T) => entityLabel(row),
    cell: (row: T) => <IdentityCell entity={row} weight={opts?.weight?.(row)} />,
  };
}
