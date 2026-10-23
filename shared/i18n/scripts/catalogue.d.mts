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
export declare function messageProblem(msg: string): string | undefined;
export declare function translationProblem(enMessage: string, message: string): string | undefined;
export declare function extractMessageRefs(
  code: string,
  fileName?: string,
  namespaces?: Set<string>,
): { explicit: Set<string>; candidates: Set<string> };
export declare function extractMessageIds(code: string, fileName?: string, namespaces?: Set<string>): Set<string>;
export declare function scanUsage(
  srcDirs: string[],
  namespaces?: Set<string>,
): Promise<{ used: Map<string, string>; problems: string[] }>;
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
