// Where a floating element sits against its anchor. The wrappers' own type, so
// a caller never names the library's (#2485); it auto-flips and shifts to stay
// in view either way.
export type Placement =
  | "top"
  | "top-start"
  | "top-end"
  | "bottom"
  | "bottom-start"
  | "bottom-end"
  | "left"
  | "left-start"
  | "left-end"
  | "right"
  | "right-start"
  | "right-end";
