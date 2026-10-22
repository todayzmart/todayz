import type { MessageFormatElement } from '@formatjs/icu-messageformat-parser';

export declare const PSEUDO_LOCALE: 'en-XA';
export declare const PSEUDO_GROWTH: number;
export declare function accent(s: string): string;
export declare function padding(length: number): string;
export declare function pseudoAst(ast: MessageFormatElement[]): MessageFormatElement[];
