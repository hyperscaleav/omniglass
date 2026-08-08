// system_color: a deterministic identity color for a system, derived from its
// uuid, so a system reads as itself wherever more than one is shown at once
// (the tree's lead icon, a component's system column, a location's health
// rollup) with no hand-maintained per-system map. Same shape as
// tagcolor.ts's tagHue: only the HUE crosses into CSS, as the --sys-h custom
// property on the .og-system-dot swatch (app.css); lightness and chroma stay
// theme tokens there (reusing .tag-pill's --tag-l / --tag-c, since both need
// a hash-derived hue that stays legible in both themes).
//
// Unlike a tag key, system.id is uuidv7() (db/migrations/20260627120000_init.sql),
// whose leading 48 bits are a millisecond timestamp. Devseed mints every
// system in one boot, so a hash reading only a PREFIX of the id would land
// every seeded system within the same few hundred milliseconds at nearly the
// same hue: exactly the estate the /ship-slice screenshots show. hueFor
// hashes the WHOLE string (FNV-1a, the same hash tagHue uses) so the random
// tail, not the shared timestamp prefix, drives the hue.
//
// tagcolor.ts's curated TAG_HUES ramp is deliberately not reused here: it has
// only 12 entries, five of which fall inside the reserved bands below,
// leaving too few for an estate-sized set of systems to stay distinguishable.
// A system walks the full 0-359 wheel instead, stepping past any band it
// lands in.
//
// Five hues are already spoken for by the theme's semantic tokens (success,
// warning, error, primary, info; web/src/app.css's --color-* values, dark
// block then light). A derived system hue must skip those bands, or a system
// could land the exact hue of a health badge and read as an accidental
// status color instead of an identity one.
//
// These bands are OKLCH degrees, not sRGB/HSL ones, because the CSS that
// consumes --sys-h renders in OKLCH (app.css's .og-system-dot:
// oklch(var(--tag-l) var(--tag-c) var(--sys-h))). The two colour spaces
// disagree by tens of degrees for these particular hexes (task 9 review,
// finding C2: an HSL-derived band left three of five semantic hues
// uncovered, and about 28% of systems wore a status-coloured dot). Computed
// by converting every --color-{success,warning,error,primary,info} hex in
// both theme blocks to OKLCH (Björn Ottosson's sRGB -> OKLab -> OKLCH,
// https://bottosson.github.io/posts/oklab/) and padding each theme pair by
// roughly 10 degrees on both sides:
//   error   dark #f0676b ~20.9°, light #d6494d ~22.6°  -> [10, 33)
//   warning dark #f0b232 ~80.5°, light #b9791a ~70.2°  -> [60, 91)
//   success dark #3ecf8e ~159.4°, light #16a06b ~160.3° -> [149, 171)
//   primary both themes #21cab9 ~184.0° (accent sits at ~183-185° too)
//                                                        -> [173, 195)
//   info    dark #5b8def ~262.5°, light #3567d6 ~263.4° -> [252, 274)
// system_color.test.ts verifies this independently: it converts the same
// hexes to OKLCH with its OWN from-scratch implementation (never importing
// this band list), so a future edit here that drifts from app.css's real
// colours fails the test rather than agreeing with it.
const RESERVED_HUE_BANDS: readonly (readonly [number, number])[] = [
  [10, 33], // error
  [60, 91], // warning
  [149, 171], // success
  [173, 195], // primary / accent
  [252, 274], // info
];

function inReservedBand(hue: number): boolean {
  return RESERVED_HUE_BANDS.some(([lo, hi]) => hue >= lo && hue < hi);
}

// The golden angle (360 * (1 - 1/phi)): irrational relative to 360, so
// repeated stepping never re-enters a short cycle and never lands multiple
// starting hues on the same escaped value. A fixed small step (the previous
// shape, +17) does not have this property when a band's width approaches the
// step size: most hashes landing anywhere in a ~20-degree band step to
// nearly the same handful of exit points, visibly clustering dots on
// whichever hue sits just past the band (task 9 review, finding C2, which
// measured a 2.8x over-representation at exactly --color-primary's hue for
// this reason). The golden angle spreads escapees from anywhere in a band
// across the whole remaining circle instead.
const GOLDEN_ANGLE = 360 * (1 - (Math.sqrt(5) - 1) / 2); // ~137.5077...

// hueFor derives a hue from a system's WHOLE uuid (never a prefix; see the
// header note), then steps by the golden angle, as many times as it takes,
// past any reserved band the raw hash lands in.
export function hueFor(id: string): number {
  let h = 0x811c9dc5; // FNV offset basis, matching tagHue
  for (let i = 0; i < id.length; i++) {
    h ^= id.charCodeAt(i);
    h = Math.imul(h, 0x01000193); // FNV prime, imul keeps it 32-bit
  }
  let hue = (h >>> 0) % 360;
  let guard = 0;
  // Five bands covering ~33% of the circle: a fixed cap of 30 steps leaves a
  // failure probability of roughly 0.33^30, unreachable in practice, while
  // still terminating the loop deterministically for any input.
  while (inReservedBand(hue) && guard < 30) {
    hue = (hue + GOLDEN_ANGLE) % 360;
    guard++;
  }
  return Math.round(hue * 10) / 10;
}
