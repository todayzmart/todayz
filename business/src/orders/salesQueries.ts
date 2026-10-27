// Counter sales (D-268): the store's POS sales from every device, read from the server. The filters
// live in the /orders URL (`view=sales&range=7d&payment=upi…`), so back, reload and links keep them.
// Days are IST calendar days; the server turns `from`/`to` into IST bounds.
import { ApiError } from '@todayz/api-client';
import { exportPosSales, getPosSale, getPosSalesSummary, listPosSales, reversePosSale } from '@todayz/api-client/gen/pos/pos';
import type { ListPosSalesParams } from '@todayz/api-client/gen/model/listPosSalesParams';
import type { PosPaymentMethod } from '@todayz/api-client/gen/model/posPaymentMethod';
import type { PosSaleLinkFilter } from '@todayz/api-client/gen/model/posSaleLinkFilter';
import type { PosSaleStatus } from '@todayz/api-client/gen/model/posSaleStatus';
import { useInfiniteQuery, useQuery } from '@tanstack/react-query';
import type { CurrentStore } from './store';

export const RANGES = ['today', 'yesterday', '7d', '30d', 'custom'] as const;
export type SalesRange = (typeof RANGES)[number];
export const PAYMENTS = ['cash', 'upi', 'card'] as const satisfies readonly PosPaymentMethod[];
export const STATUSES = ['completed', 'reversed'] as const satisfies readonly PosSaleStatus[];
export const LINKS = ['linked', 'pending', 'failed', 'none'] as const satisfies readonly PosSaleLinkFilter[];

/** The Counter sales part of the /orders search. Absent means "all" (and `today` for the range). */
export interface SalesSearch {
  view?: 'sales';
  range?: SalesRange;
  from?: string;
  to?: string;
  payment?: (typeof PAYMENTS)[number];
  status?: (typeof STATUSES)[number];
  link?: (typeof LINKS)[number];
  offline?: true;
  cashier?: string;
  /** The sale open in the detail panel. */
  sale?: string;
}

const DAY = /^\d{4}-\d{2}-\d{2}$/;
const ULID = /^[0-9A-HJKMNP-TV-Z]{26}$/;
const pick = <T extends string>(all: readonly T[], v: unknown): T | undefined => (all.includes(v as T) ? (v as T) : undefined);

/** Reads the Counter sales keys from a raw search; anything unknown is dropped. */
export function parseSalesSearch(s: Record<string, unknown>): SalesSearch {
  if (s.view !== 'sales') return {};
  const out: SalesSearch = { view: 'sales' };
  const range = pick(RANGES, s.range);
  if (range) out.range = range;
  if (typeof s.from === 'string' && DAY.test(s.from)) out.from = s.from;
  if (typeof s.to === 'string' && DAY.test(s.to)) out.to = s.to;
  const payment = pick(PAYMENTS, s.payment);
  if (payment) out.payment = payment;
  const status = pick(STATUSES, s.status);
  if (status) out.status = status;
  const link = pick(LINKS, s.link);
  if (link) out.link = link;
  if (s.offline === true || s.offline === 'true') out.offline = true;
  if (typeof s.cashier === 'string' && ULID.test(s.cashier)) out.cashier = s.cashier;
  if (typeof s.sale === 'string' && ULID.test(s.sale)) out.sale = s.sale;
  return out;
}

/** Today's date in IST as YYYY-MM-DD. */
export function istToday(nowMs: number): string {
  return new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Kolkata', year: 'numeric', month: '2-digit', day: '2-digit' }).format(nowMs);
}

/** A YYYY-MM-DD day moved by n days. */
export function addDays(day: string, n: number): string {
  const d = new Date(`${day}T00:00:00Z`);
  d.setUTCDate(d.getUTCDate() + n);
  return d.toISOString().slice(0, 10);
}

/** The IST days a range covers, inclusive, given today's IST date. A custom range with a missing end is open on that side. */
export function rangeDays(s: SalesSearch, today: string): { from?: string; to?: string } {
  switch (s.range ?? 'today') {
    case 'today':
      return { from: today, to: today };
    case 'yesterday':
      return { from: addDays(today, -1), to: addDays(today, -1) };
    case '7d':
      return { from: addDays(today, -6), to: today };
    case '30d':
      return { from: addDays(today, -29), to: today };
    case 'custom': {
      const out: { from?: string; to?: string } = {};
      if (s.from) out.from = s.from;
      if (s.to) out.to = s.to;
      return out;
    }
  }
}

/** The query params the list, summary and export share. */
export function salesParams(s: SalesSearch, today: string): Omit<ListPosSalesParams, 'limit' | 'cursor'> {
  const p: Omit<ListPosSalesParams, 'limit' | 'cursor'> = { ...rangeDays(s, today) };
  if (s.payment) p.payment = s.payment;
  if (s.status) p.status = s.status;
  if (s.link) p.link = s.link;
  if (s.offline) p.offline = true;
  if (s.cashier) p.cashier = s.cashier;
  return p;
}

/** Stats and the CSV are store-wide: owner and manager, as Insights (D-268). */
export function canSeeSalesTotals(role: string): boolean {
  return role === 'owner' || role === 'manager';
}

/** Reversing a sale is manager and above (`pos.reverse`, D-08). */
export function canReverseSale(role: string): boolean {
  return role === 'owner' || role === 'manager';
}

/** The bill's last six characters: enough to tell bills apart at a glance; the panel shows the whole id. */
export function shortId(id: string): string {
  return id.slice(-6);
}

export const salesKey = (sid: string) => ['pos-sales', sid] as const;
const PAGE = 25;

export function useSalesList(store: CurrentStore, params: ReturnType<typeof salesParams>) {
  return useInfiniteQuery({
    queryKey: [...salesKey(store.sid), 'list', params],
    queryFn: ({ pageParam }) => listPosSales(store.bid, store.sid, { ...params, limit: PAGE, ...(pageParam ? { cursor: pageParam } : {}) }),
    initialPageParam: null as string | null,
    getNextPageParam: (last) => last.nextCursor ?? null,
  });
}

export function useSalesSummary(store: CurrentStore, params: ReturnType<typeof salesParams>, enabled: boolean) {
  return useQuery({
    queryKey: [...salesKey(store.sid), 'summary', params],
    queryFn: () => getPosSalesSummary(store.bid, store.sid, params),
    enabled,
  });
}

export function useSale(store: CurrentStore, id: string | undefined) {
  return useQuery({
    queryKey: [...salesKey(store.sid), 'sale', id],
    queryFn: () => getPosSale(store.bid, store.sid, id ?? ''),
    enabled: id !== undefined,
    retry: (n, err) => !(err instanceof ApiError && (err.status === 404 || err.status === 403)) && n < 1,
  });
}

export function reverseSale(store: CurrentStore, saleId: string, reason: string, idempotencyKey: string) {
  return reversePosSale(store.bid, store.sid, saleId, { reason }, { idempotencyKey });
}

/** The CSV of the filtered range; never carries a phone (the server writes no phone column). */
export function salesCsv(store: CurrentStore, params: ReturnType<typeof salesParams>): Promise<string> {
  return exportPosSales(store.bid, store.sid, params);
}
