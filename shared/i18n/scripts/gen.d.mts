import type { MessageFormatElement } from '@formatjs/icu-messageformat-parser';

export declare function loadApp(app: string): Promise<Record<string, string>>;
export declare function compile(
  messages: Record<string, string>,
  opts?: { pseudo?: boolean },
): Record<string, MessageFormatElement[]>;

export declare function loadLocaleMessages(
  app: string,
  locale: string,
  opts?: { drafts?: boolean; dir?: string; onWarn?: (message: string) => void },
): Promise<Record<string, string>>;

export declare function draftsFromEnv(env?: Record<string, string | undefined>): boolean;
