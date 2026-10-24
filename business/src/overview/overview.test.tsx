import en from '@todayz/i18n/compiled/business/en.json';
import { asMessages, FORMAT_LOCALE, formatMoney } from '@todayz/i18n';
import { fireEvent, render, screen, within } from '@testing-library/react';
import { IntlProvider } from 'react-intl';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { renderAt } from '../test/render';
import { NARROW_QUERY } from '../shell/useNarrow';
import { GreetBand } from './GreetBand';
import { SAMPLE_DATA, averageBill, sampleOverview } from './sample';
import { SetupCard } from './SetupCard';
import type { SetupProgress } from './types';

const messages = asMessages(en);

function renderOverview(path = '/') {
  return renderAt(path, { messages });
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
  it('renders the greeting, shop, KPIs, new orders, chart and top items', async () => {
    renderOverview();
    expect(await screen.findByRole('heading', { level: 1, name: 'Good evening, Kavya' })).toBeInTheDocument();
    expect(screen.getByText(/· Sri Lakshmi Provision Stores ·/)).toHaveClass('tz-page-sub');
    expect(screen.getByRole('region', { name: 'At a glance' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'New orders' })).toBeInTheDocument();
    expect(screen.getAllByTestId('countdown')).toHaveLength(2);
    expect(screen.getByRole('heading', { name: 'Sales by hour' })).toBeInTheDocument();
    expect(screen.getByRole('table', { name: /top items/i })).toBeInTheDocument();
    expect(screen.getByText('Synced 6:40 pm')).toHaveAttribute('role', 'status');
  });

  it('shows the Sample data pill in the greeting band while the sample flag is on', async () => {
    expect(SAMPLE_DATA).toBe(true);
    renderOverview();
    await screen.findByRole('heading', { level: 1 });
    expect(screen.getByText('Sample data')).toHaveClass('tz-pill');
  });

  it('hides the Sample data pill for real data', () => {
    render(
      <IntlProvider locale={FORMAT_LOCALE} messages={messages}>
        <GreetBand data={{ ...sampleOverview('today'), isSample: false }} period="today" onPeriod={() => undefined} />
      </IntlProvider>,
    );
    expect(screen.getByRole('heading', { level: 1 })).toBeInTheDocument();
    expect(screen.queryByText('Sample data')).toBeNull();
  });

  it('shows at most 4 insights, one per kind, highest severity first (D-99)', async () => {
    renderOverview();
    const card = await insightsCard();
    const kinds = kindsOf(card);
    expect(kinds.length).toBeLessThanOrEqual(4);
    expect(new Set(kinds).size).toBe(kinds.length);
    expect(kinds).toEqual(['selling_fast', 'going_to_waste', 'sitting_on_shelf', 'wanted_nearby']);
    // The second selling-fast item (Tata Salt) is dropped: one row per kind.
    expect(within(card).queryByText(/Tata Salt/)).toBeNull();
  });

  it('links each insight action to its prefilled screen (06-screens deep links)', async () => {
    renderOverview();
    const card = await insightsCard();
    const href = (name: string) => within(card).getByRole('link', { name: new RegExp(`^${name}`) }).getAttribute('href');
    expect(href('Restock')).toMatch(/^\/stock\?restock=[^&]+&qty=\d+$/);
    expect(href('Flash offer')).toMatch(/^\/offers\?offer=flash&batches=/);
    expect(href('Create offer')).toMatch(/^\/offers\?offer=item&item=[^&]+&pct=\d+$/);
    expect(href('Add product')).toMatch(/^\/stock\?add=/);
  });

  it('Not useful hides the row and the next row moves up, still one per kind', async () => {
    renderOverview();
    let card = await insightsCard();
    // Hiding Amul (selling fast) brings the next selling-fast item, Tata Salt, into its place.
    const [first] = within(card).getAllByRole('button', { name: /^Not useful/ });
    if (!first) throw new Error('no Not useful button');
    fireEvent.click(first);
    card = await insightsCard();
    expect(within(card).queryByText(/Amul Taaza/)).toBeNull();
    expect(within(card).getByText(/Tata Salt/)).toBeInTheDocument();
    expect(await screen.findByText(/Hidden for this visit/)).toBeInTheDocument();
    // Hiding the last row (wanted nearby) lets the lowest kind, bought together, fill in.
    const last = within(card).getAllByRole('button', { name: /^Not useful/ }).at(-1);
    if (!last) throw new Error('no Not useful button');
    fireEvent.click(last);
    const kinds = kindsOf(await insightsCard());
    expect(kinds).toHaveLength(4);
    expect(new Set(kinds).size).toBe(4);
    expect(kinds.at(-1)).toBe('bought_together');
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
    expect(screen.getByRole('img', { name: /6 pm/ })).toHaveAccessibleName(/₹6,400/);
  });

  it('formats money from paise with Indian digit grouping', async () => {
    expect(formatMoney(48_260_00, 'dashboard')).toBe('₹48,260');
    expect(formatMoney(1_00_000_00, 'dashboard')).toBe('₹1,00,000');
    expect(averageBill(48_260_00, 142)).toBe(340_00);
    renderOverview();
    const kpis = await screen.findByRole('region', { name: 'At a glance' });
    expect(within(kpis).getByText('₹48,260')).toBeInTheDocument();
    expect(within(kpis).getByText('₹340')).toBeInTheDocument();
    const table = screen.getByRole('table', { name: /top items/i });
    expect(within(table).getByText('₹5,202')).toBeInTheDocument();
    expect(screen.getByText('96 sold · ₹9,304')).toBeInTheDocument();
  });

  it('switches the period without a network call', async () => {
    renderOverview();
    await screen.findByRole('heading', { level: 1 });
    fireEvent.click(screen.getByRole('radio', { name: '7 days' }));
    expect((await screen.findAllByText('vs previous 7 days')).length).toBeGreaterThan(0);
    const kpis = screen.getByRole('region', { name: 'At a glance' });
    expect(within(kpis).queryByText('₹48,260')).toBeNull();
    expect(within(kpis).getByText('Sales')).toBeInTheDocument();
  });

  it('keeps the sample setup card out of the desktop sidebar', async () => {
    // The sidebar frames every page, real ones included; a sample "80%" there would contradict the
    // real shop. The card returns at the sidebar foot when SAMPLE_DATA goes false (W3-03).
    mockWidth(1280);
    renderOverview();
    await screen.findByRole('heading', { level: 1, name: /^Good / });
    expect(document.querySelector('nav.tz-side .tz-side-card')).toBeNull();
    expect(screen.queryByText(/^Shop setup/)).toBeNull();
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

  it.each<[string, SetupProgress]>([
    ['complete', { percent: 100, next: null }],
    ['over 100', { percent: 120, next: null }],
    ['not a number', { percent: Number.NaN, next: 'openingHours' }],
  ])('hides the setup card when setup is %s', (_, setup) => {
    const { container } = render(
      <IntlProvider locale={FORMAT_LOCALE} messages={messages}>
        <SetupCard setup={setup} />
      </IntlProvider>,
    );
    expect(container).toBeEmptyDOMElement();
  });
});
