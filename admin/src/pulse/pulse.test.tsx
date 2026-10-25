import type { AdminPulse } from '@todayz/api-client/gen/model/adminPulse';
import type { AdminPulseDay } from '@todayz/api-client/gen/model/adminPulseDay';
import type { AdminPulseMap } from '@todayz/api-client/gen/model/adminPulseMap';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { NOW } from '../test/fixtures';
import { json, renderAt, type Handler } from '../test/render';
import { cityRows, countChange, dayLabel, funnelSteps, percent, rateChange, TREND, trendModel } from './model';
import { validatePulseSearch } from './search';

const STORE = '01JSTORE000000000000000AA1';

function day(date: string, over: Partial<AdminPulseDay> = {}): AdminPulseDay {
  return {
    date,
    gmvPaise: 1_000_00,
    posGmvPaise: 400_00,
    orders: 10,
    completedOrders: 8,
    activeCustomers: 50,
    newCustomers: 5,
    liveStores: 12,
    searches: 200,
    zeroResultSearches: 20,
    ...over,
  };
}

function pulse(over: Partial<AdminPulse> = {}): AdminPulse {
  return {
    range: '7d',
    from: '2026-10-19',
    to: '2026-10-25',
    asOf: '2026-10-25T06:25:00Z',
    kpis: {
      gmvPaise: { value: 7_240_00, previous: 6_000_00 },
      onlineGmvPaise: { value: 4_440_00, previous: 4_000_00 },
      posGmvPaise: { value: 2_800_00, previous: 2_000_00 },
      orders: { value: 70, previous: 80 },
      completedOrders: { value: 58, previous: 60 },
      successRateBp: { value: 8286, previous: 7500 },
      activeCustomers: { value: 340, previous: null },
      transactingCustomers: { value: 61, previous: 55 },
      liveStores: { value: 12, previous: 12 },
      activeStores: { value: 15, previous: 14 },
      newStores: { value: 2, previous: 0 },
      searches: { value: 1400, previous: 1200 },
      searchSuccessRateBp: { value: 9000, previous: 9000 },
      d7RetentionBp: { value: null, previous: null },
    },
    trend: ['19', '20', '21', '22', '23', '24', '25'].map((d) => day(`2026-10-${d}`)),
    funnel: { searches: 1400, searchesWithResults: 1260, storeViews: 600, ordersPlaced: 70, ordersAccepted: 63, ordersCompleted: 58 },
    leaks: [
      { status: 'expired', reason: 'not_accepted', count: 6 },
      { status: 'rejected', reason: 'out_of_stock', count: 4 },
    ],
    retention: {
      d1Bp: 4100,
      d7Bp: 2200,
      d30Bp: null,
      cohorts: [{ cohortWeek: '2026-10-19', size: 30, weeksBp: [10000, 3300] }],
    },
    credits: { issued: 500, spent: 320, expired: 10 },
    zeroResultSearches: [{ queryNorm: 'paneer', searches: 14 }],
    storesNeedingAttention: [
      {
        storeId: STORE,
        name: 'Lakshmi Stores',
        city: 'Bengaluru',
        reason: 'not_accepting',
        expiredUnaccepted: 4,
        strikes30d: 1,
        acceptRateBp: 6000,
        stockUpdatedAt: null,
      },
    ],
    serverNow: NOW,
    ...over,
  };
}

const MAP: AdminPulseMap = {
  range: '7d',
  from: '2026-10-19',
  to: '2026-10-25',
  stores: [
    { storeId: STORE, name: 'Lakshmi Stores', city: 'Bengaluru', status: 'live', lat: 12.97, lng: 77.59, live: true },
    { storeId: '01JSTORE000000000000000AA2', name: 'Ravi Mart', city: 'Bengaluru', status: 'paused', lat: 12.98, lng: 77.6, live: false },
    { storeId: '01JSTORE000000000000000AA3', name: 'Sri Ganesh', city: 'Mysuru', status: 'live', lat: 12.3, lng: 76.64, live: true },
  ],
  demand: [{ cell: '8860145b49fffff', searches: 40, zeroResultSearches: 6 }],
  serverNow: NOW,
};

function api(body: (range: string) => AdminPulse = () => pulse()): Handler {
  return (c) => {
    const range = new URLSearchParams(c.search).get('range') ?? '7d';
    if (c.path === '/v1/admin/pulse') return json(200, body(range));
    if (c.path === '/v1/admin/pulse/map') return json(200, { ...MAP, range });
    return undefined;
  };
}

describe('Pulse model', () => {
  it('compares counts in whole percent and rates in points, and says nothing without a previous figure', () => {
    expect(countChange({ value: 120, previous: 100 })).toEqual({ percent: 20, direction: 'up' });
    expect(countChange({ value: 70, previous: 80 })).toEqual({ percent: 13, direction: 'down' });
    expect(countChange({ value: 5, previous: 5 })).toEqual({ percent: 0, direction: 'flat' });
    expect(countChange({ value: 5, previous: 0 })).toBeUndefined();
    expect(countChange({ value: 5, previous: null })).toBeUndefined();
    expect(rateChange({ value: 8286, previous: 7500 })).toEqual({ points: 7.9, direction: 'up' });
    expect(rateChange({ value: null, previous: 7500 })).toBeUndefined();
    expect(percent(8286)).toBe('82.9%');
    expect(percent(null)).toBe('—');
  });

  it('scales bars to the highest day and labels every day up to 10, then about 7 including the last', () => {
    const m = trendModel([day('2026-10-24', { orders: 5 }), day('2026-10-25', { orders: 10 })], 'orders');
    expect(m.max).toBe(10);
    expect(m.bars[1]).toMatchObject({ height: TREND.bottom - TREND.top, y: TREND.top });
    expect(m.bars[0]?.height).toBe((TREND.bottom - TREND.top) / 2);
    expect([...m.labelled].sort()).toEqual([0, 1]);
    const long = trendModel(
      Array.from({ length: 30 }, (_, i) => day(`2026-10-${String(i + 1).padStart(2, '0')}`)),
      'orders',
    );
    expect(long.labelled.has(29)).toBe(true);
    expect(long.labelled.size).toBeLessThanOrEqual(8);
    expect(trendModel([day('2026-10-25', { orders: 0 })], 'orders').bars[0]?.height).toBe(0);
  });

  it('gives each funnel step its share of searches and of the step before', () => {
    const steps = funnelSteps(pulse().funnel);
    expect(steps[0]).toMatchObject({ key: 'searches', share: 100, ofPreviousBp: null });
    expect(steps[1]).toMatchObject({ key: 'searchesWithResults', share: 90, ofPreviousBp: 9000 });
    expect(steps[5]).toMatchObject({ key: 'ordersCompleted', ofPreviousBp: 9206 });
  });

  it('counts stores and live stores per city, biggest first', () => {
    expect(cityRows(MAP)).toEqual([
      { city: 'Bengaluru', stores: 2, live: 1 },
      { city: 'Mysuru', stores: 1, live: 1 },
    ]);
  });

  it('reads IST dates without the device time zone, and keeps only known ranges in the URL', () => {
    expect(dayLabel('2026-10-01')).toBe('1 Oct');
    expect(validatePulseSearch({ range: '30d' })).toEqual({ range: '30d' });
    expect(validatePulseSearch({ range: '7d' })).toEqual({});
    expect(validatePulseSearch({ range: 'year' })).toEqual({});
  });
});

describe('A-02 Pulse', () => {
  it('shows the KPIs with their change, the funnel, leaks, retention and credits', async () => {
    renderAt('/', { handlers: [api()] });
    expect(await screen.findByText('₹7,240')).toBeInTheDocument();
    expect(screen.getByText('₹4,440 online · ₹2,800 counter')).toBeInTheDocument();
    expect(screen.getByText('Up 21%')).toBeInTheDocument();
    expect(screen.getByText('Down 13%')).toBeInTheDocument();
    expect(screen.getByText('Up 7.9 pts')).toBeInTheDocument();
    expect(screen.getByText('82.9%')).toBeInTheDocument();
    // No previous figure: no change shown, and a rate with nothing to divide reads as a dash.
    expect(screen.getByText('340')).toBeInTheDocument();
    expect(screen.getAllByText('—').length).toBeGreaterThan(0);
    expect(screen.getByRole('img', { name: /GMV per day from 19 Oct to 25 Oct/ })).toBeInTheDocument();

    const funnel = screen.getByRole('region', { name: 'Funnel' });
    expect(within(funnel).getByText('Found something')).toBeInTheDocument();
    expect(within(funnel).getByText('· 47.6%')).toBeInTheDocument();

    const leaks = screen.getByRole('region', { name: 'Where orders leak' });
    expect(within(leaks).getByText('· Not accepted in time')).toBeInTheDocument();
    expect(within(leaks).getByText('· 60.0%')).toBeInTheDocument();

    const retention = screen.getByRole('region', { name: 'Retention' });
    expect(within(retention).getByText('41.0%')).toBeInTheDocument();
    expect(within(retention).getByText('33.0%')).toBeInTheDocument();

    expect(within(screen.getByRole('region', { name: 'Credits' })).getByText('320')).toBeInTheDocument();
    expect(within(screen.getByRole('region', { name: 'Searches that found nothing' })).getByText('paneer')).toBeInTheDocument();

    const cities = await screen.findByRole('cell', { name: 'Mysuru' });
    expect(cities).toBeInTheDocument();
    expect(screen.getByText('1 area with 10 or more searches, 40 searches in all.')).toBeInTheDocument();
  });

  it('keeps the period in the URL and asks the API for it', async () => {
    const { calls, router } = renderAt('/', { handlers: [api((range) => pulse({ range: range as AdminPulse['range'] }))] });
    await screen.findByText('₹7,240');
    fireEvent.click(screen.getByRole('radio', { name: '30 days' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ range: '30d' }));
    await waitFor(() => expect(calls.some((c) => c.path === '/v1/admin/pulse' && c.search === '?range=30d')).toBe(true));
    expect(screen.getAllByText('vs the 30 days before').length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole('radio', { name: '7 days' }));
    await waitFor(() => expect(router.state.location.search).toEqual({}));
  });

  it('switches the trend figure', async () => {
    renderAt('/', { handlers: [api()] });
    await screen.findByText('₹7,240');
    fireEvent.click(screen.getByRole('radio', { name: 'Searches' }));
    expect(screen.getByRole('img', { name: /Searches per day from 19 Oct to 25 Oct, highest 200/ })).toBeInTheDocument();
  });

  it('links a store needing attention for support and above, and only names it for an analyst', async () => {
    const first = renderAt('/', { handlers: [api()], roles: ['support'] });
    expect(await screen.findByRole('link', { name: 'Lakshmi Stores' })).toHaveAttribute('href', `/stores/${STORE}`);
    expect(screen.getByText('Not accepting')).toBeInTheDocument();
    first.unmount();

    renderAt('/', { handlers: [api()], roles: ['analyst'] });
    await screen.findByText('₹7,240');
    const table = screen.getByRole('table', { name: 'Stores needing attention' });
    expect(within(table).getByText('Lakshmi Stores')).toBeInTheDocument();
    expect(within(table).queryByRole('link')).toBeNull();
  });

  it('says when the rollups have not run yet and shows empty states', async () => {
    renderAt('/', {
      handlers: [api(() => pulse({ asOf: null, leaks: [], zeroResultSearches: [], storesNeedingAttention: [], retention: { d1Bp: null, d7Bp: null, d30Bp: null, cohorts: [] } }))],
    });
    expect(await screen.findByText(/Not updated yet/)).toBeInTheDocument();
    expect(screen.getByText('No lost orders in this period.')).toBeInTheDocument();
    expect(screen.getByText('No customer cohorts yet.')).toBeInTheDocument();
    expect(screen.getByText('No store needs attention.')).toBeInTheDocument();
  });

  it('shows the error with a retry when the API fails', async () => {
    renderAt('/');
    expect(await screen.findByRole('alert')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument();
  });
});
