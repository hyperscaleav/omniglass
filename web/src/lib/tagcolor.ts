// tagcolor: a deterministic, out-of-the-box color for a tag key. A tag has no
// stored color (that is a later slice); its hue is derived from the key name so
// the same key is the same color everywhere, with no backend and no picker.
//
// The hue indexes a small curated ramp rather than the raw wheel: the entries
// are hand-spread but prune the muddy low-chroma yellow-green band (~60-90deg)
// that never reads well as a label, so every key lands on a legible color. Only
// the hue crosses into CSS (as the --tag-h custom property on .tag-pill); the
// lightness and chroma live in per-theme tokens, so one hue themes correctly and
// stays contrast-safe in light and dark.

// The curated hue ramp (degrees). Distinct, evenly spread, yellow-green pruned.
export const TAG_HUES = [262, 220, 199, 186, 168, 150, 130, 45, 30, 12, 340, 300];

// tagHue maps a key name to a stable hue in TAG_HUES via an FNV-1a hash. Pure and
// deterministic: tagHue(k) === tagHue(k) for all k, across sessions and machines.
export function tagHue(key: string): number {
  let h = 0x811c9dc5; // FNV offset basis
  for (let i = 0; i < key.length; i++) {
    h ^= key.charCodeAt(i);
    h = Math.imul(h, 0x01000193); // FNV prime, imul keeps it 32-bit
  }
  return TAG_HUES[(h >>> 0) % TAG_HUES.length];
}
