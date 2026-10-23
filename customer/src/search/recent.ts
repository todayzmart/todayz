// Recent searches stay on this device only (never sent anywhere), newest first.
import { browserStore } from '@todayz/i18n';

const KEY = 'todayz.recentSearches';
const MAX = 8;

export function getRecent(): string[] {
  try {
    const raw = browserStore()?.getItem(KEY);
    const v: unknown = raw ? JSON.parse(raw) : [];
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string').slice(0, MAX) : [];
  } catch {
    return [];
  }
}

function save(list: string[]): void {
  try {
    browserStore()?.setItem(KEY, JSON.stringify(list));
  } catch {
    // storage blocked: recent searches are just not remembered
  }
}

export function addRecent(query: string): string[] {
  const q = query.trim();
  if (q.length < 2) return getRecent();
  const list = [q, ...getRecent().filter((x) => x.toLowerCase() !== q.toLowerCase())].slice(0, MAX);
  save(list);
  return list;
}

export function clearRecent(): void {
  save([]);
}
