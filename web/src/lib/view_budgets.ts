// The three budgets (#838).
//
// Each answers the same question about a different resource: can this view
// afford the thing, given how much of it there is. They exist because the
// alternative is a setting, and a setting asks an operator to know something
// the view already knows. At 23 rooms every room can wear its name; at 602 it
// cannot, at any type size, and no amount of tuning changes that. So the view
// works it out and the manual settings become overrides for a screenshot or a
// projector rather than the normal way to drive it.
//
// All three are pure arithmetic about available space. A renderer consuming
// them has nothing left to decide, which is what keeps the decision in one
// place instead of once per renderer.

export type LabelMode = "auto" | "always" | "off";

// Rooms in view past which names stop fitting.
//
// Twenty-four, measured against the real console rather than guessed. A room
// name needs about 7rem and a card is about 16rem, so a card fits two labelled
// groups per row; at a screen of eight or so cards that is roughly two dozen
// names before they start running into each other and reading as one string.
//
// The number also produces the behaviour the design wants without a second
// rule: at the fleet level the whole estate's rooms are in view, so names are
// off and the card header carries the identity, and drilling into one card
// drops the count to a handful so the names come back on their own.
export const LABEL_CEILING = 24;

// labelsAffordable: the label budget. Auto is the arithmetic, always and off
// are the operator overriding it in either direction.
export function labelsAffordable(roomsInView: number, mode: LabelMode, ceiling: number = LABEL_CEILING): boolean {
  if (mode === "always") return true;
  if (mode === "off") return false;
  return ceiling > 0 && roomsInView <= ceiling;
}

// roomBoxesAffordable: a room box costs more width than the name inside it, so
// it goes first. It also stays an operator toggle, because at low counts the
// boxes are a taste question and the names are not.
export function roomBoxesAffordable(
  roomsInView: number,
  mode: LabelMode,
  wanted: boolean,
  ceiling: number = LABEL_CEILING,
): boolean {
  if (!wanted) return false;
  if (mode === "always") return true;
  if (mode === "off") return false;
  return ceiling > 0 && roomsInView <= ceiling;
}

// The smallest tile worth drawing, in square pixels, and the shortest side it
// may have. Below either, a tile is not a thing an operator can point at; it
// is texture that reads as data.
export const MIN_TILE_AREA = 240;
export const MAX_TILES = 48;

export type FoldResult<T> = { drawn: T[]; folded: T[] };

// foldToBudget: the area budget. Anything that would draw smaller than a tile
// somebody can click folds into a remainder the caller labels, rather than
// becoming a one-pixel sliver. The cap is the second half of the same rule:
// past a few dozen tiles a frame stops being readable however big each one is.
//
// Sorted by weight descending, so the remainder is always the smallest things
// and never an arbitrary tail.
export function foldToBudget<T>(
  items: T[],
  weightOf: (item: T) => number,
  poolPx: number,
  opts: { minArea?: number; maxTiles?: number } = {},
): FoldResult<T> {
  const minArea = opts.minArea ?? MIN_TILE_AREA;
  const maxTiles = opts.maxTiles ?? MAX_TILES;
  if (items.length === 0) return { drawn: [], folded: [] };

  const sorted = [...items].sort((a, b) => weightOf(b) - weightOf(a));
  const total = sorted.reduce((sum, i) => sum + Math.max(0, weightOf(i)), 0);
  // With no weight anywhere there is nothing to divide by, so the cap is the
  // only rule left that means anything.
  if (total <= 0) {
    return { drawn: sorted.slice(0, maxTiles), folded: sorted.slice(maxTiles) };
  }

  const drawn: T[] = [];
  const folded: T[] = [];
  for (const item of sorted) {
    const share = Math.max(0, weightOf(item)) / total;
    const estimate = share * Math.max(0, poolPx);
    if (drawn.length < maxTiles && estimate >= minArea) drawn.push(item);
    else folded.push(item);
  }

  // A frame that folds everything shows nothing at all, which is worse than
  // one crowded tile: keep the largest and fold the rest around it.
  if (drawn.length === 0) {
    drawn.push(folded.shift() as T);
  }
  return { drawn, folded };
}

export type FrameBox = { headerH: number; x: number; y: number; w: number; h: number };

// The header height a frame reserves, and the smallest frame worth putting one
// on. Below these a title would take the space its contents need.
const HEADER_H = 16;
const HEADER_MIN_W = 96;
const HEADER_MIN_H = 42;
const FRAME_PAD = 3;

// frameLayout: the z-order budget, expressed as space rather than as a rule
// somebody has to remember. A container's title is RESERVED area above its
// contents, not an overlay drawn on top of them, so a tile can never paint
// over the name of the thing it is inside. That failure is not hypothetical:
// it is what absolutely-positioned tiles appended after their frame did.
//
// Everything is integer pixels. Fractional boxes are how adjacent tiles end up
// with a seam between them or a row of overlapping edges.
export function frameLayout(w: number, h: number, pad: number = FRAME_PAD): FrameBox {
  const fw = Math.max(0, Math.floor(w));
  const fh = Math.max(0, Math.floor(h));
  // A frame too small for a title gives up the title, never the contents.
  const headerH = fw >= HEADER_MIN_W && fh >= HEADER_MIN_H ? HEADER_H : 0;
  const x = Math.min(pad, fw);
  const y = Math.min(headerH > 0 ? headerH : pad, fh);
  return {
    headerH,
    x,
    y,
    w: Math.max(0, fw - x - pad),
    h: Math.max(0, fh - y - pad),
  };
}
