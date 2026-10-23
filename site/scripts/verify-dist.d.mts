export interface VerifyOptions {
  e2e?: boolean;
  draftLocales?: readonly string[];
}
export function checkFile(rel: string, text: string, options?: VerifyOptions): string[];
export function checkDist(dist: string, options?: VerifyOptions): { files: number; problems: string[] };
export function draftLocaleProblems(rel: string, text: string, locales: readonly string[]): string[];
