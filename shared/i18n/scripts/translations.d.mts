export interface TodoItem {
  key: string;
  area: string;
  en: string;
  description: string;
  was?: string | undefined;
}
export declare function todo(dir: string, app: string, locale: string): Promise<TodoItem[]>;
export declare function importDrafts(
  dir: string,
  app: string,
  area: string,
  locale: string,
  drafts: Record<string, string>,
): Promise<number>;
export declare function approve(dir: string, app: string, area: string, locale: string, keys: string[]): Promise<number>;
