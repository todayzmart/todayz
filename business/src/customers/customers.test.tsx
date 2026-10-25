import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages } from '@todayz/i18n';
import { fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { customersHandler, ownerApi, STORE } from '../test/insightsApi';
import { renderAt, type Handler } from '../test/render';
import { validateCustomersSearch } from './search';

const messages = asMessages(en);

function renderCustomers(path: string, handlers: Handler[] = [...ownerApi(), customersHandler()]) {
  return renderAt(path, { messages, handlers });
}

const segmentOf = (calls: { path: string; search: string }[]) =>
  calls.filter((c) => c.path === `${STORE}/customers`).map((c) => new URLSearchParams(c.search).get('segment'));

describe('B-09 Customers', () => {
  it('keeps a known segment and drops anything else', () => {
    expect(validateCustomersSearch({ segment: 'slipping' })).toEqual({ segment: 'slipping' });
    expect(validateCustomersSearch({ segment: 'vip' })).toEqual({});
  });

  it('lists who bought here with masked phones only', async () => {
    const { calls } = renderCustomers('/customers');
    const table = await screen.findByRole('table', { name: 'Customers' });
    await within(table).findByText('Meera S.');
    expect(screen.getByText('2 customers bought here in the last 90 days')).toBeInTheDocument();
    expect(within(table).getByText('••••••4321')).toHaveClass('tz-sub');
    // A phone-only link shows the masked phone as its name.
    expect(within(table).getByText('••••••7788')).toBeInTheDocument();
    expect(within(table).getByText('₹6,480')).toBeInTheDocument();
    expect(within(table).getByText('4 Jan')).toBeInTheDocument();
    expect(segmentOf(calls)).toEqual(['all']);
  });

  it('opens Slipping away from the insight link and lists the regulars who stopped coming', async () => {
    const { calls } = renderCustomers('/customers?segment=slipping');
    const chips = await screen.findByRole('group', { name: 'Customer groups' });
    expect(within(chips).getByRole('button', { name: 'Slipping away' })).toHaveAttribute('aria-pressed', 'true');
    expect(await screen.findByText("1 regular customer hasn't bought in 30 days")).toBeInTheDocument();
    expect(screen.queryByText('Meera S.')).toBeNull();
    expect(segmentOf(calls)).toEqual(['slipping']);
  });

  it('switches segment through the URL', async () => {
    const { history, calls } = renderCustomers('/customers');
    const chips = await screen.findByRole('group', { name: 'Customer groups' });
    fireEvent.click(within(chips).getByRole('button', { name: 'Top spenders' }));
    await waitFor(() => expect(history.location.search).toBe('?segment=top'));
    await waitFor(() => expect(segmentOf(calls)).toContain('top'));
  });

  it('says to link customers when there are too few', async () => {
    renderCustomers('/customers', [...ownerApi(), customersHandler({ enough: false })]);
    expect(await screen.findByText('Not enough linked customers yet')).toBeInTheDocument();
    expect(screen.getByText('Link customers at the counter to see this')).toBeInTheDocument();
    expect(screen.queryByRole('table')).toBeNull();
  });

  it('loads the next page on the cursor', async () => {
    const { calls } = renderCustomers('/customers', [...ownerApi(), customersHandler({ pages: 2 })]);
    fireEvent.click(await screen.findByRole('button', { name: 'Load more' }));
    expect(await screen.findByText('Ravi K.')).toBeInTheDocument();
    expect(calls.filter((c) => c.path === `${STORE}/customers`).map((c) => new URLSearchParams(c.search).get('cursor'))).toEqual([null, '1']);
    expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull();
  });

  it('keeps a cashier out and asks nothing', async () => {
    const { calls } = renderCustomers('/customers', [...ownerApi('cashier'), customersHandler()]);
    expect(await screen.findByRole('link', { name: 'New sale' })).toBeInTheDocument();
    expect(segmentOf(calls)).toEqual([]);
  });
});
