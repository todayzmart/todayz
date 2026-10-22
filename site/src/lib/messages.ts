// Site copy from shared/i18n/messages/site (compiled by `make gen`). The site has no client JS,
// so messages are formatted at build time. Only literals and simple {arguments} are supported;
// anything richer (plural, select, tags) throws so it cannot ship half-formatted.
import en from '@todayz/i18n/compiled/site/en.json';

export type SiteKey = keyof typeof en;
type Part = { type: number; value?: unknown };
export type Catalogue = Record<string, readonly Part[]>;

const LITERAL = 0;
const ARGUMENT = 1;

export function formatMessage(
  catalogue: Catalogue,
  key: string,
  values: Readonly<Record<string, string | number>> = {},
): string {
  const parts = catalogue[key];
  if (!parts) throw new Error(`site i18n: missing message ${key}`);
  return parts
    .map((part) => {
      if (part.type === LITERAL && typeof part.value === 'string') return part.value;
      if (part.type === ARGUMENT && typeof part.value === 'string') {
        const v = values[part.value];
        if (v === undefined) throw new Error(`site i18n: ${key} needs {${part.value}}`);
        return String(v);
      }
      throw new Error(`site i18n: ${key} uses an ICU feature the static site does not format`);
    })
    .join('');
}

/** English copy for the site. */
export function t(key: SiteKey, values?: Readonly<Record<string, string | number>>): string {
  return formatMessage(en, key, values);
}
