import type { Page, PlaywrightTestConfig } from '@playwright/test';

export declare function freePort(): number;
export declare function playwrightPreset(opts: {
  command: (port: number) => string;
  testDir?: string;
  outputDir?: string;
}): PlaywrightTestConfig;
export declare function isLocalUrl(raw: string): boolean;
export interface PageGuard {
  external: string[];
  errors: string[];
  responses: { url: string; status: number }[];
}
export declare function guardPage(page: Page): Promise<PageGuard>;
export declare function expectFontLoaded(page: Page, guard: PageGuard, family: string, file: RegExp): Promise<void>;
export declare function qaDir(): string;
export declare function shot(page: Page, name: string, opts?: { fullPage?: boolean }): Promise<Buffer>;
export declare const VIEWPORTS: {
  phone: { width: number; height: number };
  desktop: { width: number; height: number };
};
export default playwrightPreset;
