// Hand-written declarations for settle.mjs (plain ESM so the capture scripts run it without a build step).
import type { Page } from "@playwright/test";
export type SettleOptions = { quietMs?: number; timeout?: number };
export function isStreamRequest(url: string, accept: string | undefined): boolean;
export function trackRequests(page: Page): { inflight: Set<unknown> };
export function settle(page: Page, opts?: SettleOptions): Promise<void>;
export function gotoSettled(page: Page, url: string, opts?: SettleOptions): Promise<void>;
