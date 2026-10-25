import type { DemandGap } from '@todayz/api-client/gen/model/demandGap';
import type { MarqueeCity } from '@todayz/api-client/gen/model/marqueeCity';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { NOW, emptyAudit } from '../test/fixtures';
import { json, problem, renderAt, type Handler } from '../test/render';
import { csvField, gapsCsv } from './csv';
import { canApproveStrip } from './perm';
import { validateDemandSearch } from './search';

const BLR = '01JC1TY0000000000000000B01';
const HYD = '01JC1TY0000000000000000H01';

function gap(over: Partial<DemandGap> = {}): DemandGap {
  return {
    cityId: BLR,
    cityName: 'Bengaluru',
    productId: '01JPR0D0000000000000000P01',
    itemName: 'Amul taaza 1 L',
    category: 'Dairy',
    customers: 4,
    asks: 6,
    shopsAsked: 9,
    lastAskedAt: '2026-10-25T05:30:00Z',
    ...over,
  };
}

const GAPS = [gap(), gap({ cityId: HYD, cityName: 'Hyderabad', productId: null, itemName: 'Pooja kumkum', category: null, customers: 3, asks: 3 })];

function city(over: Partial<MarqueeCity> = {}): MarqueeCity {
  return {
    cityId: BLR,
    cityName: 'Bengaluru',
    citySlug: 'bengaluru',
    latest: {
      builtOn: '2026-10-25',
      status: 'proposed',
      decidedAt: null,
      items: [
        { productId: '01JPR0D0000000000000000P01', productName: 'Amul taaza 1 L', category: 'Dairy', searchers: 12, searchesTotal: 30, rank: 1, status: 'proposed' },
        { productId: '01JPR0D0000000000000000P02', productName: 'Maggi 70 g', category: 'Snacks', searchers: 8, searchesTotal: 11, rank: 2, status: 'proposed' },
      ],
    },
    approved: null,
    ...over,
  };
}

function api(opts: { gaps?: DemandGap[]; cities?: MarqueeCity[]; decide?: number } = {}): Handler {
  let cities = opts.cities ?? [city()];
  return (c) => {
    if (c.path === '/v1/admin/demand-gaps') return json(200, { items: opts.gaps ?? GAPS, days: 7, minCustomers: 3, serverNow: NOW });
    if (c.path === '/v1/admin/site-marquee' && c.method === 'GET') return json(200, { cities, minSearchers: 5, serverNow: NOW });
    if (c.path.startsWith('/v1/admin/site-marquee/') && c.method === 'PUT') {
      if (opts.decide) return problem(opts.decide, opts.decide === 409 ? 'invalid_transition' : 'validation_failed');
      const body = c.body as { builtOn: string; productIds: string[] };
      cities = cities.map((x) => {
        if (!x.latest) return x;
        const items = x.latest.items.map((i) => ({ ...i, status: body.productIds.includes(i.productId) ? ('approved' as const) : ('rejected' as const) }));
        const build = { ...x.latest, status: 'approved' as const, decidedAt: NOW, items };
        return { ...x, latest: build, approved: build };
      });
      return json(200, cities[0]);
    }
    if (c.path === '/v1/admin/audit-log/subject') return json(200, emptyAudit);
    return undefined;
  };
}

describe('demand helpers', () => {
  it('keeps only a known tab and a well-formed city', () => {
    expect(validateDemandSearch({ tab: 'strip', city: BLR, x: 1 })).toEqual({ tab: 'strip', city: BLR });
    expect(validateDemandSearch({ tab: 'other', city: 'blr' })).toEqual({});
  });

  it('lets only catalogue and admins approve the strip', () => {
    expect(canApproveStrip(['catalogue'])).toBe(true);
    expect(canApproveStrip(['admin'])).toBe(true);
    expect(canApproveStrip(['field_agent', 'analyst'])).toBe(false);
  });

  it('writes a CSV that no spreadsheet runs as a formula', () => {
    expect(csvField('=SUM(A1)')).toBe("'=SUM(A1)");
    expect(csvField('a, "b"')).toBe('"a, ""b"""');
    const out = gapsCsv([gap({ itemName: '+91 hack' })]).split('\n');
    expect(out[0]).toBe('city,product,product_id,category,customers,asks,shops_asked,last_asked_at');
    expect(out[1]).toContain("'+91 hack");
    expect(out.join('\n')).not.toMatch(/phone|customer_id|name,/);
  });
});

describe('A-23 demand gaps', () => {
  it('lists every city, then filters one through the URL, with the rule from settings', async () => {
    const { router } = renderAt('/demand', { roles: ['field_agent'], handlers: [api()] });
    const table = await screen.findByRole('table', { name: 'Demand gaps' });
    expect(await within(table).findByText('Amul taaza 1 L')).toBeInTheDocument();
    expect(within(table).getByText('Pooja kumkum')).toBeInTheDocument();
    expect(within(table).getByText('Not in the catalogue')).toBeInTheDocument();
    expect(within(table).getByRole('columnheader', { name: 'City' })).toBeInTheDocument();
    expect(screen.getByText(/at least 3 customers in the last 7 days/)).toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: 'Site strip' })).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText('City'), { target: { value: HYD } });
    await waitFor(() => expect(router.state.location.search).toEqual({ city: HYD }));
    await waitFor(() => expect(within(table).queryByText('Amul taaza 1 L')).not.toBeInTheDocument());
    expect(within(table).getByText('Pooja kumkum')).toBeInTheDocument();
    expect(within(table).queryByRole('columnheader', { name: 'City' })).not.toBeInTheDocument();
  });

  it('shows an empty table and disables export when there are no gaps', async () => {
    renderAt('/demand', { roles: ['analyst'], handlers: [api({ gaps: [] })] });
    expect(await screen.findByText('No gaps right now')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Export CSV' })).toBeDisabled();
  });

  it('sends a field agent asking for the strip back to the gaps', async () => {
    const { calls } = renderAt('/demand?tab=strip', { roles: ['field_agent'], handlers: [api()] });
    expect(await screen.findByRole('table', { name: 'Demand gaps' })).toBeInTheDocument();
    expect(calls.some((c) => c.path === '/v1/admin/site-marquee')).toBe(false);
  });
});

describe('A-23 site strip', () => {
  it('approves the proposal without the products left out', async () => {
    const { calls, router } = renderAt('/demand', { roles: ['catalogue'], handlers: [api()] });
    fireEvent.click(await screen.findByRole('tab', { name: 'Site strip' }));
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: 'strip' }));
    const card = await screen.findByRole('region', { name: 'Bengaluru' });
    expect(within(card).getByText('Proposed on 2026-10-25')).toBeInTheDocument();
    expect(within(card).getByText(/12 searchers · Dairy/)).toBeInTheDocument();
    expect(within(card).getByText('Not approved yet: the site shows the curated list.')).toBeInTheDocument();
    expect(screen.getByText(/at least 5 signed-in customers/)).toBeInTheDocument();
    // The city's decisions, audited as `site_marquee` and keyed by the city, show beside the strip.
    expect(await within(card).findByRole('complementary', { name: 'History' })).toBeInTheDocument();
    const audits = () => calls.filter((c) => c.path === '/v1/admin/audit-log/subject' && c.search.includes('subjectType=site_marquee')).length;
    await waitFor(() => expect(audits()).toBe(1));
    expect(calls.find((c) => c.path === '/v1/admin/audit-log/subject')?.search).toContain(`subjectId=${BLR}`);

    fireEvent.click(within(card).getByRole('checkbox', { name: /Maggi 70 g/ }));
    fireEvent.click(within(card).getByRole('button', { name: 'Approve 1 product' }));
    await waitFor(() => expect(screen.getByText('Site strip for Bengaluru approved with 1 product')).toBeInTheDocument());
    const put = calls.find((c) => c.method === 'PUT');
    expect(put?.path).toBe(`/v1/admin/site-marquee/${BLR}`);
    expect(put?.body).toEqual({ builtOn: '2026-10-25', decision: 'approve', productIds: ['01JPR0D0000000000000000P01'] });
    expect(put?.headers.get('Idempotency-Key')).toMatch(/^[0-9A-HJKMNP-TV-Z]{26}$/);

    const after = await screen.findByRole('region', { name: 'Bengaluru' });
    expect(await within(after).findByText(/On the site: built 2026-10-25/)).toBeInTheDocument();
    expect(within(after).getByText('No new proposal waiting.')).toBeInTheDocument();
    expect(within(after).getAllByRole('listitem').map((li) => li.textContent)).toEqual(['Amul taaza 1 L']);
    await waitFor(() => expect(audits()).toBeGreaterThan(1));
  });

  it('will not approve an empty strip', async () => {
    const { calls } = renderAt('/demand?tab=strip', { roles: ['admin'], handlers: [api()] });
    const card = await screen.findByRole('region', { name: 'Bengaluru' });
    for (const box of within(card).getAllByRole('checkbox')) fireEvent.click(box);
    expect(within(card).getByRole('button', { name: 'Approve 0 products' })).toBeDisabled();
    expect(calls.some((c) => c.method === 'PUT')).toBe(false);
  });

  it('says so when the proposal is no longer the latest', async () => {
    renderAt('/demand?tab=strip', { roles: ['super_admin'], handlers: [api({ decide: 409 })] });
    const card = await screen.findByRole('region', { name: 'Bengaluru' });
    fireEvent.click(within(card).getByRole('button', { name: 'Approve 2 products' }));
    expect(await screen.findByText('This changed while you were looking. Reload and check it again.')).toBeInTheDocument();
  });

  it('filters the cities through the URL', async () => {
    const hyd = city({ cityId: HYD, cityName: 'Hyderabad', citySlug: 'hyderabad', latest: null });
    const { router } = renderAt('/demand?tab=strip', { roles: ['catalogue'], handlers: [api({ cities: [city(), hyd] })] });
    expect(await screen.findByRole('region', { name: 'Hyderabad' })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('City'), { target: { value: HYD } });
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: 'strip', city: HYD }));
    await waitFor(() => expect(screen.queryByRole('region', { name: 'Bengaluru' })).not.toBeInTheDocument());
    expect(within(screen.getByRole('region', { name: 'Hyderabad' })).getByText('No new proposal waiting.')).toBeInTheDocument();
  });
});
