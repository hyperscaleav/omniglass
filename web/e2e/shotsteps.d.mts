// Hand-written declarations for shotsteps.mjs (plain ESM so the capture runs it
// without a build step; shotsteps.spec.ts imports it through this surface).
import type { Page } from "@playwright/test";

export type ShotStep = { action: string; selector?: string; value?: string; timeout?: number; message?: string; count?: number; modifiers?: string[] };
export function normalizeStep(step: unknown): ShotStep;
export function runStep(page: Page, id: string, step: ShotStep): Promise<void>;
