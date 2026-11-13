import type { BladeEdit } from "./blades";

// The leave guard: a page that hosts edit slots (a workspace's Configure faces)
// asks it before it drops them, so switching tab with unsaved drafts is a
// question rather than a silent loss. The page creates one guard, each face
// tracks its slot into it, and the tab rail asks confirmLeave() before it moves.
// It guards the page's own facets only; the router as a whole is not guarded.
export type LeaveGuard = {
  // Track a slot; the returned function untracks it (call it on cleanup).
  track: (slot: Pick<BladeEdit, "dirty">) => () => void;
  // True when the operator may leave: nothing is dirty, or they confirmed
  // discarding what is.
  confirmLeave: () => boolean;
};

export const LEAVE_PROMPT = "Discard your unsaved changes?";

export function createLeaveGuard(): LeaveGuard {
  const slots = new Set<Pick<BladeEdit, "dirty">>();
  return {
    track: (slot) => {
      slots.add(slot);
      return () => slots.delete(slot);
    },
    confirmLeave: () => ![...slots].some((s) => s.dirty()) || confirm(LEAVE_PROMPT),
  };
}
