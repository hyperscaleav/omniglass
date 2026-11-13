// Hand-written declarations for shotdiff.mjs (plain ESM so the capture scripts
// run it without a build step; the gate test imports it through this surface).
export const DEFAULT_MAX_RATIO: number;
export function compareShot(
  committedBuf: Buffer | Uint8Array,
  freshBuf: Buffer | Uint8Array,
  opts?: { maxRatio?: number },
):
  | { status: "ok" | "changed"; ratio: number; diffPixels: number }
  | { status: "size"; a: { width: number; height: number }; b: { width: number; height: number } };
