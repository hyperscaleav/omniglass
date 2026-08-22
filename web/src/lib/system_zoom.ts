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
import { sortAlarms } from "./alarms";

export type FleetHealth = components["schemas"]["FleetHealthOutputBody"];
export type HealthRole = components["schemas"]["HealthRoleBody"];
export type EffectiveRole = components["schemas"]["EffectiveRoleBody"];

export type SlotOccupant = {
  name: string;
  // The component's id, so the page can open its leaf: names repeat across
  // rooms (every huddle has a videobar-1), ids do not. Empty only when the
  // fleet projection carries no dot for the name, which the wire never does
  // for an assigned member; the page then renders the name without a link.
  componentId: string;
  // Down means the occupant's OWN verdict is outage: it holds the slot and
  // cannot serve it, the opposite of a slot nobody filled.
  down: boolean;
  // The labels of the OTHER systems this occupant also serves, empty for an
  // unshared component.
  sharedWith: string[];
  // The declared position label, where the role labels its positions.
  positionLabel?: string;
  // The declared 1-based slot number this occupant holds, defaulting to its
  // assignment order when the role declares none: the map join key (#791).
  position: number;
};

export type SlotVM = {
  name: string;
  label: string;
  quorum: number;
  satisfying: number;
  short: number;
  // Occupants beyond quorum: the redundancy an operator wants to see racked.
  spare: number;
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
      const position = pos ?? i + 1;
      return {
        name,
        componentId: dot?.component ?? "",
        position,
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
      spare: r.spare ?? 0,
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

// One active alarm on the system, joined for the strip (#785): the health
// read already carries every alarm on a down occupant per role; this flattens
// the ACTIVE roles' alarms (an inactive alternate's alarms did not move the
// verdict, and rendering one would contradict the header), joins the role's
// label and the component's uuid (via the projection's dots, names repeat
// across rooms), and orders worst first.
export type AlarmRow = {
  id: string;
  severity: string;
  message: string;
  component: string;
  componentId: string | null;
  roleLabel: string;
  raisedAt: string;
};

export function alarmRows(health: FleetHealth, view: FleetView, systemId: string): AlarmRow[] {
  const self = (view.systems ?? []).find((s) => s.id === systemId);
  // The alarm wire names its component by UUID (healthAlarmBody carries
  // a.ComponentID); the projection's dots translate it to the name an
  // operator recognises. An alarm on a component the projection does not
  // carry keeps the raw id as its text and renders unlinked.
  const nameById = new Map((self?.dots ?? []).map((d) => [d.component, d.name]));
  const rows: AlarmRow[] = [];
  for (const r of health.roles ?? []) {
    if (!r.active) continue;
    for (const a of r.alarms ?? []) {
      const known = nameById.has(a.component);
      rows.push({
        id: a.id,
        severity: a.severity,
        message: a.message,
        component: nameById.get(a.component) ?? a.component,
        componentId: known ? a.component : null,
        roleLabel: entityLabel(r),
        raisedAt: a.raised_at,
      });
    }
  }
  // The wire repeats an alarm under every role its down component staffs;
  // the strip is a list of alarms, not of role pairings, so the first
  // occurrence after the worst-first sort wins.
  const sorted = sortAlarms(rows.map((r) => ({ ...r, raised_at: r.raisedAt }))).map(({ raised_at: _, ...r }) => r as AlarmRow);
  const seen = new Set<string>();
  return sorted.filter((r) => (seen.has(r.id) ? false : (seen.add(r.id), true)));
}

// The header's since-when (#785): the last recorded edge and its age. Null
// when nothing was ever recorded, and the header then says nothing rather
// than inventing a duration.
export function sinceOf(health: FleetHealth, now: number): { ts: string; ms: number } | null {
  const t = health.transitions ?? [];
  if (t.length === 0) return null;
  const last = t[t.length - 1];
  return { ts: last.ts, ms: now - Date.parse(last.ts) };
}

// The components-first body (#790): the operator's question is "what is in
// this room and is it fine", so the unit of the page is the COMPONENT, its
// role a badge on the card. Role-level structure appears only where it says
// something a badge cannot: a role that wants more than one occupant, a role
// that is short, or a role nobody staffed. In the 99% case (one role, one
// healthy occupant) the room is a flat row of cards.
export type CardRole = { label: string; position?: string };
export type ComponentCard = {
  componentId: string;
  name: string;
  down: boolean;
  shared: string[];
  roles: CardRole[];
  noRole: boolean;
};
export type RoleGroup = {
  name: string;
  label: string;
  quorum: number;
  satisfying: number;
  short: number;
  spare: number;
  impact: string;
  // The occupant names, in position order; empty for an unstaffed role.
  members: string[];
  // The occupants' full cards, badges included: a grouped component has ONE
  // home on the page (inside its group), so a bar that also answers an
  // ungrouped role renders here wearing both badges.
  memberCards: ComponentCard[];
};
export type SystemBody = { cards: ComponentCard[]; groups: RoleGroup[] };

export function componentCards(vm: SystemZoomVM): SystemBody {
  // Active slots only: the build not in use stays in the standard editor.
  const activeSlots: SlotVM[] = [
    ...vm.unconditional.filter((s) => s.active),
    ...vm.choices.flatMap((c) => c.alternates.filter((a) => a.active).flatMap((a) => a.roles.filter((r) => r.active))),
  ];

  const groups: RoleGroup[] = [];
  const byComponent = new Map<string, ComponentCard>();
  const card = (o: { componentId: string; name: string; down?: boolean; sharedWith?: string[] }): ComponentCard => {
    let c = byComponent.get(o.name);
    if (!c) {
      c = { componentId: o.componentId, name: o.name, down: o.down ?? false, shared: o.sharedWith ?? [], roles: [], noRole: false };
      byComponent.set(o.name, c);
    }
    return c;
  };

  for (const slot of activeSlots) {
    // Grouping earns its chrome: quorum beyond one, or anything short of
    // whole (an unstaffed or short role is outstanding work the badge
    // cannot carry).
    const grouped = slot.quorum > 1 || slot.short > 0 || slot.occupants.length === 0;
    if (grouped) {
      groups.push({
        name: slot.name,
        label: slot.label,
        quorum: slot.quorum,
        satisfying: slot.satisfying,
        short: slot.short,
        spare: slot.spare,
        impact: slot.impact,
        members: slot.occupants.map((o) => o.name),
        memberCards: [],
      });
    }
    for (const o of slot.occupants) {
      const c = card(o);
      c.down = c.down || o.down;
      c.shared = [...new Set([...c.shared, ...o.sharedWith])];
      c.roles.push({ label: slot.label, position: o.positionLabel });
    }
  }
  for (const m of vm.noRole) {
    const c = card(m);
    c.noRole = true;
  }
  // A grouped role's occupants render inside the group, full card and all
  // badges; the flat row holds only components whose every role is ungrouped
  // (plus the no-role members).
  const groupedNames = new Set(groups.flatMap((g) => g.members));
  for (const g of groups) g.memberCards = g.members.map((n) => byComponent.get(n)!).filter(Boolean);
  const cards = [...byComponent.values()].filter((c) => !groupedNames.has(c.name));
  return { cards, groups };
}
