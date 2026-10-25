import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages, FORMAT_LOCALE } from '@todayz/i18n';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { createIntl } from 'react-intl';
import { describe, expect, it } from 'vitest';
import { analyticsHandler, insights, ownerApi, STORE } from '../test/insightsApi';
import { renderAt, type Handler } from '../test/render';
import { insightId, type InsightData } from '../overview/types';
import { needsLine } from './needs';
import { validateAnalyticsSearch } from './search';

const messages = asMessages(en);
const intl = createIntl({ locale: FORMAT_LOCALE, messages });

function renderAnalytics(path: string, handlers: Handler[] = [...ownerApi(), analyticsHandler()]) {
  return renderAt(path, { messages, handlers });
}

function needs(kind: InsightData['kind'], params: Record<string, unknown>): InsightData {
  const d = { kind, subjectRef: null, severity: null, evidence: { state: 'needs_data', needs: { key: `insights.needs.${kind}`, params } }, facts: null, action: null, text: null } as const;
  return { ...d, id: insightId(d) };
}

describe('B-12 Analytics search', () => {
  it('keeps a known tab and kind and drops anything else', () => {
    expect(validateAnalyticsSearch({ tab: 'insights', kind: 'selling_fast' })).toEqual({ tab: 'insights', kind: 'selling_fast' });
    expect(validateAnalyticsSearch({ tab: 'nope', kind: 'drop table' })).toEqual({});
    expect(validateAnalyticsSearch({ kind: 7 })).toEqual({});
  });
});

describe('needsLine (D-99 minimum evidence)', () => {
  it('says what each kind needs, from the server params', () => {
    expect(needsLine(intl, needs('selling_fast', { minUnits: 10 }))).toBe('Shows once an item sells 10 or more in the period');
    // One decimal under 10 km (formatDistance).
    expect(needsLine(intl, needs('wanted_nearby', { minSearches: 5, radiusM: 2000 }))).toBe(
      'Shows once there are 5 or more searches within 2.0 km for something you do not list',
    );
    expect(needsLine(intl, needs('regulars_slipping', { minLinked: 10 }))).toBe('Link customers at the counter to see this');
    expect(needsLine(intl, needs('priced_above_nearby', { minStores: 3 }))).toBe('Shows once 3 or more shops nearby list the same products');
    expect(needsLine(intl, needs('stock_check_due', {}))).toBe('Not enough data yet');
  });
});

describe('B-12 Analytics: Insights tab', () => {
  it('groups every insight by kind with actions, and kinds below their minimum say what they need', async () => {
    const { calls } = renderAnalytics('/analytics?tab=insights');
    const fast = await screen.findByRole('region', { name: 'Selling fast' });
    expect(within(fast).getAllByRole('listitem')).toHaveLength(2);
    expect(within(fast).getAllByRole('link', { name: /^Restock/ }).map((l) => l.getAttribute('href'))).toEqual([
      '/stock?restock=i1&qty=40',
      '/stock?restock=i2&qty=10',
    ]);
    const slipping = screen.getByRole('region', { name: 'Regulars slipping away' });
    expect(slipping).toHaveTextContent('Link customers at the counter to see this');
    expect(within(slipping).queryByRole('link')).toBeNull();
    expect(within(slipping).queryByRole('button')).toBeNull();
    expect(screen.getAllByRole('region').map((r) => r.getAttribute('aria-labelledby') && r.querySelector('h2')?.textContent)).toEqual([
      'Selling fast',
      'Sitting on the shelf',
      'Going to waste',
      'Wanted nearby',
      'Regulars slipping away',
    ]);
    expect(calls.some((c) => c.path === `${STORE}/insights` && new URLSearchParams(c.search).get('all') === '1')).toBe(true);
  });

  it('filters by kind through the URL', async () => {
    const { history, calls } = renderAnalytics('/analytics?tab=insights');
    const filter = await screen.findByRole('group', { name: 'Insight kinds' });
    fireEvent.click(within(filter).getByRole('button', { name: 'Wanted nearby' }));
    await waitFor(() => expect(history.location.search).toBe('?tab=insights&kind=wanted_nearby'));
    await waitFor(() => expect(screen.queryByRole('region', { name: 'Selling fast' })).toBeNull());
    expect(screen.getByRole('region', { name: 'Wanted nearby' })).toBeInTheDocument();
    expect(calls.some((c) => new URLSearchParams(c.search).get('kind') === 'wanted_nearby')).toBe(true);
    // The filter still lists every kind of the unfiltered answer.
    expect(within(filter).getByRole('button', { name: 'Selling fast' })).toBeInTheDocument();
  });

  it('Not useful dismisses one row and toasts until when', async () => {
    const { calls } = renderAnalytics('/analytics?tab=insights');
    const fast = await screen.findByRole('region', { name: 'Selling fast' });
    const [first] = within(fast).getAllByRole('button', { name: 'Not useful' });
    if (!first) throw new Error('no Not useful button');
    fireEvent.click(first);
    expect(await screen.findByText('Hidden until 5 Feb')).toBeInTheDocument();
    expect(within(fast).getAllByRole('listitem')).toHaveLength(1);
    expect(calls.find((c) => c.path === `${STORE}/insights/dismiss`)?.body).toEqual({ kind: 'selling_fast', subjectRef: 'i1' });
  });

  it('says so when nothing fires', async () => {
    renderAnalytics('/analytics?tab=insights', [...ownerApi().slice(0, 2), insights({ all: [] })]);
    expect(await screen.findByText('Nothing needs you right now')).toBeInTheDocument();
  });
});

describe('B-12 Analytics: Sales tab', () => {
  it('shows the totals, charts, funnel, top items, mix, repeat rate and ratings for 7 days', async () => {
    const { calls } = renderAnalytics('/analytics');
    expect(await screen.findByRole('heading', { level: 1, name: 'Analytics' })).toBeInTheDocument();
    const totals = await screen.findByRole('region', { name: 'Totals' });
    // 28 × ₹900 online + 28 × ₹3,100 counter.
    expect(within(totals).getByText('₹1,12,000')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Sales by day' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Online orders' })).toBeInTheDocument();
    expect(screen.getByText('Half your orders are accepted within 1 min 35 s')).toBeInTheDocument();
    expect(screen.getByText('Shops nearby: 2 min 20 s')).toBeInTheDocument();
    expect(screen.getByRole('table', { name: 'Top items' })).toHaveTextContent('Amul Taaza');
    expect(screen.getByText('35%')).toBeInTheDocument();
    expect(screen.getByText('14 of 40 linked customers came back')).toBeInTheDocument();
    expect(screen.getByText('4.5 ★ · 6 reviews')).toBeInTheDocument();
    expect(calls.find((c) => c.path === `${STORE}/analytics`)?.search).toBe('range=7d');
  });

  it('asks again for a longer period', async () => {
    const { calls } = renderAnalytics('/analytics');
    await screen.findByRole('region', { name: 'Totals' });
    fireEvent.click(screen.getByRole('radio', { name: '30 days' }));
    await waitFor(() => expect(calls.some((c) => c.path === `${STORE}/analytics` && c.search === 'range=30d')).toBe(true));
  });

  it('says to link customers when the repeat rate has too few', async () => {
    renderAnalytics('/analytics', [...ownerApi(), analyticsHandler({ repeat: { customers: 3, repeatCustomers: 1, repeatPct: null } })]);
    expect(await screen.findByText('Link customers at the counter to see this')).toBeInTheDocument();
  });

  it('keeps a cashier out and asks nothing', async () => {
    const { calls } = renderAnalytics('/analytics', [...ownerApi('cashier'), analyticsHandler()]);
    expect(await screen.findByRole('link', { name: 'New sale' })).toBeInTheDocument();
    expect(calls.some((c) => c.path === `${STORE}/analytics`)).toBe(false);
  });
});
