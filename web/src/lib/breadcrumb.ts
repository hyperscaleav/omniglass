// The breadcrumb's ellipsis rule as arithmetic (#633): the last two crumbs
// never shrink, and every earlier one may ellipsise down to a 26px stub. Kept
// pure so the rule is pinned once here and the Breadcrumb component only
// applies the styles; a rule scattered through CSS is the drift this replaces
// (two hand-rolled copies existed before the primitive).

// A Crumb is what a caller hands the Breadcrumb component: a label, a stable
// key, and optionally a title (the full text behind an ellipsised stub) and an
// href (the router address it leads to, so it renders as a real link). The
// last crumb is conventionally the current page and carries no href.
export type Crumb = {
  key: string;
  label: string;
  title?: string;
  href?: string;
};

// crumbLayout returns per-crumb flex styles for a trail of `count` crumbs. The
// last two hold their intrinsic width; the rest shrink first, to a stub wide
// enough to read as "something is here" (26px, the prototype's measure).
export function crumbLayout(count: number): { flex: string; minWidth: string }[] {
  return Array.from({ length: count }, (_, i) =>
    i >= count - 2 ? { flex: "none", minWidth: "auto" } : { flex: "0 1 auto", minWidth: "26px" },
  );
}

// collapseTrail decides which crumbs of the top bar's trail stay visible beside
// the title and the controls (#2575): a trail whose labels fit the budget shows
// whole; a longer one keeps the first crumb (where Back up lands) and the last
// (the parent) and folds every crumb between into an ellipsis menu. Pure, so
// the rule is pinned here and the bar only renders it.
export const TRAIL_BUDGET = 72;
export function collapseTrail(crumbs: Crumb[], budget = TRAIL_BUDGET): { head: Crumb[]; hidden: Crumb[]; tail: Crumb[] } {
  const chars = crumbs.reduce((n, c) => n + c.label.length + 3, 0);
  if (crumbs.length < 3 || chars <= budget) return { head: crumbs, hidden: [], tail: [] };
  return { head: crumbs.slice(0, 1), hidden: crumbs.slice(1, -1), tail: crumbs.slice(-1) };
}
