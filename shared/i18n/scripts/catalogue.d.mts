import type { MessageFormatElement } from '@formatjs/icu-messageformat-parser';

export declare const TRANSLATED_LOCALES: string[];
export declare const REVIEW_STATES: string[];
export declare function enHash(msg: string): string;
export interface EnglishEntry {
  message: string;
  description: string;
  file: string;
}
export interface TranslationEntry {
  message?: string;
  review?: string;
  enHash?: string;
  file: string;
}
export declare function loadEnglish(messagesDir: string, app: string): Promise<Record<string, EnglishEntry>>;
export declare function loadTranslations(
  messagesDir: string,
  app: string,
  locale: string,
): Promise<Record<string, TranslationEntry>>;
export declare function argSignature(ast: MessageFormatElement[], acc?: Set<string>): Set<string>;
export declare function extractMessageIds(source: string): Set<string>;
export declare function scanUsage(srcDirs: string[]): Promise<Map<string, string>>;
export interface LintStats {
  total: number;
  reviewed: number;
  machine: number;
  missing: number;
  stale: number;
}
export declare function lintApp(opts: {
  messagesDir: string;
  app: string;
  srcDirs?: string[];
  locales?: string[];
}): Promise<{ errors: string[]; warnings: string[]; stats: Record<string, LintStats> }>;
