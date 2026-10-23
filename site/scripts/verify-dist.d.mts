export function checkFile(rel: string, text: string, options?: { e2e?: boolean }): string[];
export function checkDist(dist: string, options?: { e2e?: boolean }): { files: number; problems: string[] };
