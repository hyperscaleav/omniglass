// Hand-written declarations for fontguard.mjs (plain ESM so the capture script
// runs it without a build step; the guard test imports it through this surface).
import type { Page } from "@playwright/test";

export interface RequiredFace {
  family: string;
  weight: string;
  style: "normal" | "italic";
}

export interface ProbedFace extends RequiredFace {
  /** how many @font-face rules declare this face (one per unicode-range split) */
  declared: number;
  /** at least one of them reached status "loaded" */
  loaded: boolean;
  /** at least one of them reached status "error" */
  errored?: boolean;
}

export const REQUIRED_FACES: RequiredFace[];
export function fontFailures(input: {
  faces: ProbedFace[];
  failedRequests?: string[];
}): string[];
export function watchFontRequests(page: Page): string[];
export function assertFontsRendered(
  page: Page,
  shotId: string,
  failedRequests?: string[],
): Promise<void>;
