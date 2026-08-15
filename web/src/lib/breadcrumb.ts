// The breadcrumb's ellipsis rule as arithmetic (#633): the last two crumbs
// never shrink, and every earlier one may ellipsise down to a 26px stub. Kept
// pure so the rule is pinned once here and the Breadcrumb component only
// applies the styles; a rule scattered through CSS is the drift this replaces
// (two hand-rolled copies existed before the primitive).

// A Crumb is what a caller hands the Breadcrumb component: a label, a stable
// key, and optionally a title (the full text behind an ellipsised stub) and a
// click. The last crumb is conventionally the current page and carries no
// onClick.
export type Crumb = {
  key: string;
  label: string;
  title?: string;
  onClick?: () => void;
};

// crumbLayout returns per-crumb flex styles for a trail of `count` crumbs. The
// last two hold their intrinsic width; the rest shrink first, to a stub wide
// enough to read as "something is here" (26px, the prototype's measure).
export function crumbLayout(count: number): { flex: string; minWidth: string }[] {
  return Array.from({ length: count }, (_, i) =>
    i >= count - 2 ? { flex: "none", minWidth: "auto" } : { flex: "0 1 auto", minWidth: "26px" },
  );
}
