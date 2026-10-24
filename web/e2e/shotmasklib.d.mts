// Hand-written declarations for shotmasklib.mjs (plain ESM so the capture runs it
// without a build step; shotmask.spec.ts imports it through this surface).
import type { Locator, Page } from "@playwright/test";

export type MaskEntry = string | { selector: string; width: number };
export type MaskBox = { x: number; y: number; width: number; height: number };
export type PlannedMask = {
  selector: string;
  width?: number;
  locator: Locator;
  rects: (MaskBox & { tag: string })[];
};

export const MASK_COLOR: string;
export function normalizeMask(entry: MaskEntry): { selector: string; width?: number };
export function planMasks(page: Page, entries: MaskEntry[]): Promise<PlannedMask[]>;
export function paintedBoxes(plan: PlannedMask[]): MaskBox[];
export function describeMask(mask: PlannedMask): string;
export function shootBaseline(page: Page, plan: PlannedMask[], opts?: { path?: string }): Promise<Buffer>;
