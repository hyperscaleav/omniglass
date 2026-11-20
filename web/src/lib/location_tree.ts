import { entityLabel } from "./entities";
import { typeRanks, type TypeShape } from "./location_type_graph";
import type { Location } from "./locations";
import type { TreeNode } from "./treeselect";

// The one place a location row becomes a tree node, so every location field
// (LocationSelect) nests, orders, and labels locations the same way: nested by
// parent_id, siblings ordered by their type's rank in the customer's own
// hierarchy (typeRanks, never a table of shipped type names) then by label.
//
// addressOf picks the address the field commits. Callers differ today (a uuid in
// the entity forms, a name for an outpost, a dotted path in the simulation and
// template forms), and this keeps each wire value as it was.
//
// pickable marks the locations a caller may choose; the rest stay in the tree,
// disabled, so the locations beneath them are still drawn under them. Dropping
// them instead would orphan their children to the root.
export interface LocationTreeOptions {
  addressOf?: (l: Location) => string;
  pickable?: (l: Location) => boolean;
}

export function locationTreeNodes(rows: Location[], types: TypeShape[], opts: LocationTreeOptions = {}): TreeNode[] {
  const rank = typeRanks(types);
  const addressOf = opts.addressOf ?? ((l: Location) => l.id);
  return rows.map((l) => {
    const node: TreeNode = {
      id: l.id ?? addressOf(l),
      value: addressOf(l),
      label: entityLabel(l),
      parentId: l.parent_id,
      rank: rank.get(l.location_type) ?? Number.MAX_SAFE_INTEGER,
    };
    if (opts.pickable && !opts.pickable(l)) node.disabled = true;
    return node;
  });
}
