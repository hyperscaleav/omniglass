import { entityLabel, type Labelled } from "./entities";

// How a system is named wherever it is listed (#2757). A company names its
// rooms ("Curie", "Huddle 3") and a building addresses them (the location,
// "Hq Tower 12 01"), and a real fleet has both, so the console shows both, the
// name first and the address beside it, muted.
//
// The address is the label of the location the system sits at. It is left out
// where it would say nothing new: a system with no location, and one named for
// its room (the dev fleet's "Boardroom A" at "Boardroom A"), which reads once.
//
// Pure, so the rule is held without a DOM; components/SystemName.tsx renders it
// and the text-only surfaces (a picker's option, a search row) join it here.
export function systemAddress(system: Labelled, location: Labelled | null | undefined): string | undefined {
  if (!location) return undefined;
  const address = entityLabel(location);
  return address === entityLabel(system) ? undefined : address;
}

// systemWithAddress is the name and its address as one line of text, for a
// surface that cannot lay out two spans (a <select> option, an aria-label).
export function systemWithAddress(system: Labelled, location: Labelled | null | undefined): string {
  const address = systemAddress(system, location);
  return address ? `${entityLabel(system)} · ${address}` : entityLabel(system);
}
