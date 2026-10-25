// C-25 signed out: the sheet's fields wait here while the customer signs in (C-10), then the same
// sheet opens again with them. Session storage only, and only the item, quantity and need-by: nothing
// about the person.
import type { AskNeedWhen } from '@todayz/api-client/gen/model/askNeedWhen';

export interface AskDraft {
  /** The page the sheet was on, so only that page reopens it. */
  path: string;
  /** Which slot on the page: the product, the typed words, or the shop. */
  slot: string;
  text: string;
  quantity: number;
  needBy: AskNeedWhen | null;
}

const KEY = 'todayz.askDraft';

export function saveDraft(d: AskDraft): void {
  try {
    sessionStorage.setItem(KEY, JSON.stringify(d));
  } catch {
    // Storage blocked: the sheet opens empty after sign-in.
  }
}

/** The draft for this slot on this page; it stays until clearDraft (the sheet clears it once shown). */
export function readDraft(path: string, slot: string): AskDraft | null {
  try {
    const raw = sessionStorage.getItem(KEY);
    if (!raw) return null;
    const d = JSON.parse(raw) as Partial<AskDraft>;
    if (d.path !== path || d.slot !== slot) return null;
    const quantity = Number.isInteger(d.quantity) && (d.quantity ?? 0) > 0 ? (d.quantity as number) : 1;
    const needBy = d.needBy === 'now' || d.needBy === 'today' || d.needBy === 'tomorrow' ? d.needBy : null;
    return { path, slot, text: typeof d.text === 'string' ? d.text : '', quantity, needBy };
  } catch {
    return null;
  }
}

export function clearDraft(): void {
  try {
    sessionStorage.removeItem(KEY);
  } catch {
    // Storage blocked: nothing was kept.
  }
}
