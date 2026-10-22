import type { MessageFormatElement } from '@formatjs/icu-messageformat-parser';

export declare function loadApp(app: string): Promise<Record<string, string>>;
export declare function compile(
  messages: Record<string, string>,
  opts?: { pseudo?: boolean },
): Record<string, MessageFormatElement[]>;
