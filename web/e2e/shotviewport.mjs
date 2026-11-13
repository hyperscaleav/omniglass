// The house capture viewport and the parser for shot.mjs's --viewport WxH flag.
// Plain ESM so the capture runs it without a build step; shotviewport.spec.ts
// imports it through shotviewport.d.mts.
export const HOUSE_VIEWPORT = { width: 1320, height: 860 };

// parseViewport reads "WxH" as two positive integers; anything else is null.
export function parseViewport(text) {
  const m = /^([1-9]\d*)x([1-9]\d*)$/.exec(text ?? '');
  return m ? { width: Number(m[1]), height: Number(m[2]) } : null;
}
