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
const RESERVED_HUE_BANDS: readonly (readonly [number, number])[] = [
  [340, 360], // error, wraps past 0 (dark #f0676b ~358°, light #d6494d ~358°)
  [0, 20], // the wrapped half of the error band
  [25, 55], // warning (dark #f0b232 ~40°, light #b9791a ~36°)
  [143, 167], // success (dark #3ecf8e ~153°, light #16a06b ~157°)
  [164, 184], // primary (both themes share #21cab9 ~174°)
  [205, 235], // info (dark #5b8def ~220°, light #3567d6 ~221°)
];

function inReservedBand(hue: number): boolean {
  return RESERVED_HUE_BANDS.some(([lo, hi]) => hue >= lo && hue < hi);
}

// hueFor derives a 0-359 hue from a system's WHOLE uuid (never a prefix; see
// the header note), then walks forward in fixed 17-degree steps (coprime with
// 360, so repeated stepping cannot cycle back onto a band without covering
// the hues in between) past any reserved band the raw hash lands in.
export function hueFor(id: string): number {
  let h = 0x811c9dc5; // FNV offset basis, matching tagHue
  for (let i = 0; i < id.length; i++) {
    h ^= id.charCodeAt(i);
    h = Math.imul(h, 0x01000193); // FNV prime, imul keeps it 32-bit
  }
  let hue = (h >>> 0) % 360;
  let guard = 0;
  while (inReservedBand(hue) && guard < 360) {
    hue = (hue + 17) % 360;
    guard++;
  }
  return hue;
}
