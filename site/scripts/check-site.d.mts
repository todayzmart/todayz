export function scan(html: string): { links: { tag: string; attr: string; url: string }[]; ids: Set<string> };
export function resolveInDist(dist: string, pathname: string): string | null;
export function checkLinks(dist: string): string[];
export function validateHtml(rel: string, html: string): Promise<string[]>;
export function checkSite(dist: string): Promise<string[]>;
