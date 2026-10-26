import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages, FORMAT_LOCALE, formatMoney } from '@todayz/i18n';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { IntlProvider } from 'react-intl';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { insights, overviewHandler, ownerApi, shops, STORE } from '../test/insightsApi';
import { renderAt, type Handler } from '../test/render';
import { holding } from '../test/held';
import { NARROW_QUERY } from '../shell/useNarrow';
import { SetupCard } from './SetupCard';
import type { SetupProgress } from './types';

const messages = asMessages(en);

function renderOverview(handlers: Handler[] = ownerApi(), path = '/') {
  return renderAt(path, { messages, handlers });
}

async function insightsCard() {
  return screen.findByRole('region', { name: 'Demand insights' });
}

function kindsOf(card: HTMLElement) {
  return [...card.querySelectorAll('.tz-insight')].map((el) => el.getAttribute('data-kind'));
}

function mockWidth(width: number) {
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: query === NARROW_QUERY ? width < 1024 : false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  }));
}

afterEach(() => vi.unstubAllGlobals());

describe('B-03 Overview', () => {
  it('renders the greeting, shop, KPIs, new orders, chart and top items from GET /overview', async () => {
    const { calls } = renderOverview();
    expect(await screen.findByRole('heading', { level: 1, name: 'Good evening, Kavya' })).toBeInTheDocument();
    expect(screen.getByText(/· Sri Lakshmi Provision Stores ·/)).toHaveClass('tz-page-sub');
    expect(screen.getByRole('region', { name: 'At a glance' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'New orders' })).toBeInTheDocument();
    expect(screen.getAllByTestId('countdown')).toHaveLength(2);
    expect(screen.getByRole('heading', { name: 'Sales by hour' })).toBeInTheDocument();
    expect(screen.getByRole('table', { name: /top items/i })).toBeInTheDocument();
    expect(screen.getByText('Synced 6:40 pm')).toHaveAttribute('role', 'status');
    expect(screen.queryByText('Sample data')).toBeNull();
    const asked = calls.find((c) => c.path === `${STORE}/overview`);
    expect(asked?.search).toBe('range=today');
  });

  it('shows the API card rows in order with their sentences (D-99)', async () => {
    renderOverview();
    const card = await insightsCard();
    await waitFor(() => expect(kindsOf(card)).toEqual(['selling_fast', 'going_to_waste', 'sitting_on_shelf', 'wanted_nearby']));
    expect(card).toHaveTextContent('Amul Taaza 500 ml sold 96 this week. At this pace you run out tomorrow.');
    expect(card).toHaveTextContent('4 items expire within 5 days: Bread, Curd, Paneer + 1 more.');
    expect(card).toHaveTextContent('Kissan Jam 500 g: 12 in stock (₹480 at cost), no sale in 21 days.');
    expect(within(card).getByText('Last 7 days')).toBeInTheDocument();
    expect(within(card).getByRole('link', { name: /^All insights/ })).toHaveAttribute('href', '/analytics?tab=insights');
  });

  it('links each insight action to its prefilled screen and applies nothing (06 deep links)', async () => {
    const { calls } = renderOverview();
    const card = await insightsCard();
    const href = async (name: string) => (await within(card).findByRole('link', { name: new RegExp(`^${name}`) })).getAttribute('href');
    expect(await href('Restock')).toBe('/stock?restock=i1&qty=40');
    expect(await href('Flash offer')).toBe('/offers?offer=flash&batches=bt1%2Cbt2');
    expect(await href('Create offer')).toBe('/offers?offer=item&item=i3&pct=15');
    expect(await href('Add product')).toBe('/stock?add=p9');
    expect(calls.filter((c) => c.method !== 'GET' && c.path !== '/v1/auth/refresh')).toEqual([]);
  });

  it('Not useful hides the row, sends the dismiss and says until when', async () => {
    const { calls } = renderOverview();
    const card = await insightsCard();
    const [first] = await within(card).findAllByRole('button', { name: /^Not useful/ });
    if (!first) throw new Error('no Not useful button');
    fireEvent.click(first);
    await waitFor(() => expect(within(card).queryByText(/Amul Taaza/)).toBeNull());
    expect(await screen.findByText('Hidden until 5 Feb')).toBeInTheDocument();
    const sent = calls.find((c) => c.path === `${STORE}/insights/dismiss`);
    expect(sent?.method).toBe('POST');
    expect(sent?.body).toEqual({ kind: 'selling_fast', subjectRef: 'i1' });
  });

  it('puts the row back when the dismiss fails', async () => {
    const failing: Handler = (c) => (c.path === `${STORE}/insights/dismiss` ? new Response(null, { status: 500 }) : undefined);
    renderOverview([failing, ...ownerApi()]);
    const card = await insightsCard();
    const [first] = await within(card).findAllByRole('button', { name: /^Not useful/ });
    if (!first) throw new Error('no Not useful button');
    fireEvent.click(first);
    expect(await screen.findByText('Could not hide it. Try again.')).toBeInTheDocument();
    expect(within(card).getByText(/Amul Taaza/)).toBeInTheDocument();
  });

  it('lists the setup steps left when no insight fires', async () => {
    renderOverview([shops(), overviewHandler(), insights({ card: [] })]);
    const card = await insightsCard();
    expect(await within(card).findByText('Nothing needs you right now')).toBeInTheDocument();
    const left = within(card).getByRole('list', { name: 'Setup steps left' });
    expect(within(left).getByRole('link', { name: 'Print the QR standee' })).toHaveAttribute('href', '/standee');
    expect(within(left).getAllByRole('link')).toHaveLength(1);
  });

  it('draws the hour in progress in brand and earlier hours grey', async () => {
    const { container } = renderOverview();
    await screen.findByRole('heading', { name: 'Sales by hour' });
    const highlighted = container.querySelectorAll('svg.tz-chart rect.bar-hl');
    expect(highlighted).toHaveLength(1);
    expect(highlighted[0]).toHaveAttribute('data-current', 'true');
    // 8 am to 6 pm: 11 bars, 10 of them before the current hour.
    expect(container.querySelectorAll('svg.tz-chart rect.bar-pale')).toHaveLength(10);
    expect(container.querySelector('svg.tz-chart path.line-prev')).not.toBeNull();
    expect(screen.getByRole('img', { name: /6 pm/ })).toHaveAccessibleName(/₹6,380/);
  });

  it('formats money from paise with Indian digit grouping', async () => {
    expect(formatMoney(48_260_00, 'dashboard')).toBe('₹48,260');
    expect(formatMoney(1_00_000_00, 'dashboard')).toBe('₹1,00,000');
    renderOverview();
    const kpis = await screen.findByRole('region', { name: 'At a glance' });
    expect(within(kpis).getByText('₹48,260')).toBeInTheDocument();
    expect(within(kpis).getByText('₹340')).toBeInTheDocument();
    const table = screen.getByRole('table', { name: /top items/i });
    expect(within(table).getByText('₹5,202')).toBeInTheDocument();
    expect(screen.getByText('147 sold · ₹14,506')).toBeInTheDocument();
  });

  it('asks the server again when the period changes', async () => {
    const { calls } = renderOverview();
    await screen.findByRole('heading', { level: 1 });
    fireEvent.click(screen.getByRole('radio', { name: '7 days' }));
    expect((await screen.findAllByText('vs previous 7 days')).length).toBeGreaterThan(0);
    expect(calls.some((c) => c.path === `${STORE}/overview` && c.search === 'range=7d')).toBe(true);
  });

  it('shows a cashier the screens they use and asks nothing', async () => {
    const { calls } = renderOverview(ownerApi('cashier'));
    expect(await screen.findByRole('link', { name: 'New sale' })).toHaveAttribute('href', '/sell');
    expect(screen.queryByRole('heading', { level: 1, name: /^Good / })).toBeNull();
    expect(calls.some((c) => /\/(overview|insights|analytics|customers)/.test(c.path))).toBe(false);
  });

  it('shows the error banner when the overview cannot load', async () => {
    const failing: Handler = (c) => (c.path === `${STORE}/overview` ? new Response(null, { status: 500 }) : undefined);
    renderOverview([failing, ...ownerApi()]);
    // The query retries once before it gives up.
    expect(await screen.findByText('Could not load the overview', undefined, { timeout: 4000 })).toBeInTheDocument();
  });

  it('puts the setup card at the foot of the desktop sidebar', async () => {
    mockWidth(1280);
    renderOverview();
    const title = await screen.findByText('Shop setup 80%');
    expect(title.closest('nav.tz-side')).not.toBeNull();
    expect(screen.getAllByText(/^Shop setup/)).toHaveLength(1);
    expect(screen.getByRole('link', { name: 'Finish setup' })).toHaveAttribute('href', '/standee');
  });

  it('moves the setup card under the KPIs on phones and shows the shop in the top bar', async () => {
    mockWidth(390);
    renderOverview();
    const title = await screen.findByText('Shop setup 80%');
    expect(title.closest('nav')).toBeNull();
    expect(screen.getAllByText(/^Shop setup/)).toHaveLength(1);
    const main = screen.getByRole('main');
    const bar = main.querySelector('.tz-topbar');
    if (!(bar instanceof HTMLElement)) throw new Error('no top bar');
    expect(within(bar).getByRole('img', { name: 'TodayZ' })).toHaveAttribute('src', '/icons/mark-96.png');
    expect(within(bar).getByText('Sri Lakshmi Provision Stores')).toBeInTheDocument();
    // Brand first, then Synced and New sale; no search box and no bell.
    expect(bar.textContent).toMatch(/^Sri Lakshmi Provision Stores.*Synced 6:40 pm.*New sale$/);
    expect(within(bar).queryByRole('searchbox')).toBeNull();
  });

  const done = { kyc: true, products: true, staff: true, standee: true, openingHours: true };
  it.each<[string, SetupProgress]>([
    ['complete', { percent: 100, next: null, steps: done }],
    ['over 100', { percent: 120, next: null, steps: done }],
    ['not a number', { percent: Number.NaN, next: 'openingHours', steps: { ...done, openingHours: false } }],
  ])('hides the setup card when setup is %s', (_, setup) => {
    const { container } = render(
      <IntlProvider locale={FORMAT_LOCALE} messages={messages}>
        <SetupCard setup={setup} />
      </IntlProvider>,
    );
    expect(container).toBeEmptyDOMElement();
  });
});

describe('B-03 Overview loading states', () => {
  it('Accept shows a spinner on that order while the call runs, and holds the other orders', async () => {
    renderOverview([holding((c) => c.method === 'POST' && c.path.endsWith('/accept')), ...ownerApi()]);
    const accepts = await screen.findAllByRole('button', { name: /^Accept order/ });
    expect(accepts.length).toBeGreaterThan(1);
    fireEvent.click(accepts[0] as HTMLElement);
    await waitFor(() => expect(accepts[0]).toHaveAttribute('aria-busy', 'true'));
    expect(accepts[1]).not.toHaveAttribute('aria-busy');
    expect(accepts[1]).toBeDisabled();
  });
});
