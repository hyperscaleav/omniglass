// The system zoom's view model (#636): the typed slots a system needs filled.
//
// Two reads each carry half the story and this module merges them by role
// name. The HEALTH read has the figures and the grouping facts (satisfying,
// short, down, choice, alternate, active); the ROLES read has the declaration
// (accepted types, pinned products, positions and their labels). Nothing here
// computes a figure: every number and every verdict on a card is the server's
// own, which is the console-computes-no-verdict rule the health module states.
//
// The occupant wire carries names, not ids (a role's assigned_to is the
// component's name within this system), so the shared-with chip resolves an
// occupant to its dot BY NAME within this system's own dots. Two members of
// one system sharing a name across rooms would collide here; the wire has no
// id to offer, and the first dot wins, which is the honest limit until the
// assignment wire grows one.

import type { components } from "../api/schema.gen";
import type { FleetView } from "./fleet";
import { entityLabel } from "./entities";

export type FleetHealth = components["schemas"]["FleetHealthOutputBody"];
export type HealthRole = components["schemas"]["HealthRoleBody"];
export type EffectiveRole = components["schemas"]["EffectiveRoleBody"];

export type SlotOccupant = {
  name: string;
  // Down means the occupant's OWN verdict is outage: it holds the slot and
  // cannot serve it, the opposite of a slot nobody filled.
  down: boolean;
  // The labels of the OTHER systems this occupant also serves, empty for an
  // unshared component.
  sharedWith: string[];
  // The declared position label, where the role labels its positions.
  positionLabel?: string;
};

export type SlotVM = {
  name: string;
  label: string;
  quorum: number;
  satisfying: number;
  short: number;
  impact: string;
  impaired: boolean;
  active: boolean;
  choice?: string;
  alternate?: string;
  acceptedTypes: string[];
  pinnedProducts: string[];
  occupants: SlotOccupant[];
  // The kind of gap a short role has: unstaffed (nobody assigned enough, a
  // commissioning gap, reads incomplete) or occupant-down (assigned hardware
  // failing, the role's declared impact applies). none when whole.
  gap: "none" | "unstaffed" | "occupant-down";
};

export type AlternateVM = { name: string; active: boolean; roles: SlotVM[] };
export type ChoiceVM = { name: string; activeAlternate?: string; alternates: AlternateVM[] };

export type SystemZoomVM = {
  unconditional: SlotVM[];
  // choices carries only the ANSWERING build per choice. The alternate a
  // system did not choose is a configuration fact, not an operational one: a
  // room does not change build without an edit, so the losing build's roles
  // are the standard editor's to show, and rendering them here as outstanding
  // work was the noise the operator-clarity ruling removed. `activeAlternate`
  // names the build so the card can say which.
  choices: ChoiceVM[];
  // Members filling no role: in the system and accounted for, which is a
  // legitimate state, never an error.
  noRole: { componentId: string; name: string; sharedWith: string[] }[];
};

export function systemZoomVM(health: FleetHealth, declared: EffectiveRole[], view: FleetView, systemId: string): SystemZoomVM {
  const declaredByName = new Map(declared.map((r) => [r.name, r]));
  const self = (view.systems ?? []).find((s) => s.id === systemId);
  const dotsByName = new Map((self?.dots ?? []).map((d) => [d.name, d]));

  // Component id -> the labels of the other systems whose dots carry it.
  const sharedWith = (componentId: string): string[] =>
    (view.systems ?? [])
      .filter((s) => s.id !== systemId && (s.dots ?? []).some((d) => d.component === componentId))
      .map((s) => entityLabel(s));

  const toSlot = (r: HealthRole): SlotVM => {
    const decl = declaredByName.get(r.name);
    const positions = decl?.positions ?? [];
    const labels = decl?.position_labels ?? [];
    const occupants: SlotOccupant[] = (r.assigned_to ?? []).map((name, i) => {
      const dot = dotsByName.get(name);
      const pos = positions[i];
      const positionLabel = pos !== undefined && labels[pos - 1] ? labels[pos - 1] : undefined;
      return {
        name,
        down: (r.down ?? []).includes(name),
        sharedWith: dot ? sharedWith(dot.component) : [],
        positionLabel,
      };
    });
    const down = (r.down ?? []).length > 0;
    return {
      name: r.name,
      label: entityLabel(r),
      quorum: r.quorum,
      satisfying: r.satisfying,
      short: r.short,
      impact: r.impact,
      impaired: r.impaired,
      active: r.active,
      choice: r.choice,
      alternate: r.alternate,
      acceptedTypes: decl?.accepted_types ?? [],
      pinnedProducts: decl?.pinned_products ?? [],
      occupants,
      gap: !r.impaired ? "none" : down ? "occupant-down" : "unstaffed",
    };
  };

  const slots = (health.roles ?? []).map(toSlot);
  const unconditional = slots.filter((s) => !s.choice);

  const choices: ChoiceVM[] = [];
  for (const slot of slots) {
    if (!slot.choice || !slot.alternate) continue;
    // Only the build in use reaches the zoom (see SystemZoomVM.choices).
    if (!slot.active) continue;
    let choice = choices.find((c) => c.name === slot.choice);
    if (!choice) {
      choice = { name: slot.choice, activeAlternate: undefined, alternates: [] };
      choices.push(choice);
    }
    let alt = choice.alternates.find((a) => a.name === slot.alternate);
    if (!alt) {
      alt = { name: slot.alternate, active: slot.active, roles: [] };
      choice.alternates.push(alt);
    }
    alt.roles.push(slot);
    if (slot.active) choice.activeAlternate = slot.alternate;
  }
  // The answering build leads; the builds this room did not choose follow.
  for (const c of choices) c.alternates.sort((a, b) => Number(b.active) - Number(a.active) || a.name.localeCompare(b.name));

  // Members filling no role: the system's dots minus every occupant name.
  const occupied = new Set(slots.flatMap((s) => s.occupants.map((o) => o.name)));
  const noRole = (self?.dots ?? [])
    .filter((d) => !occupied.has(d.name))
    .map((d) => ({ componentId: d.component, name: d.name, sharedWith: sharedWith(d.component) }));

  return { unconditional, choices, noRole };
}
