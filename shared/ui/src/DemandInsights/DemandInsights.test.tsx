import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { StoryLink } from '../test/fixtures';
import { DemandInsights, type DemandInsightsProps } from './DemandInsights';
import { canDismissInsight, INSIGHT_LOOK, pickInsightRows, type InsightKind, type InsightRow } from './insights';

const row = (id: string, kind: InsightKind, label = id): InsightRow => ({
  id,
  kind,
  label,
  sentence: `${label} sentence`,
  action: { label: `${label} action`, href: `/insights/${id}` },
});

const rows = [
  row('a', 'expired_on_shelf', 'Expired'),
  row('b', 'selling_fast', 'Fast'),
  row('c', 'selling_fast', 'Fast again'),
  row('d', 'wanted_nearby', 'Wanted'),
  row('e', 'going_to_waste', 'Waste'),
  row('f', 'sitting_on_shelf', 'Sitting'),
];

function props(over: Partial<DemandInsightsProps> = {}): DemandInsightsProps {
  return { title: 'Demand insights', rows, empty: { title: 'Nothing needs you right now' }, ...over };
}

describe('insights helpers', () => {
  it('keeps the first row per kind, up to the limit', () => {
    expect(pickInsightRows(rows, 4).map((r) => r.id)).toEqual(['a', 'b', 'd', 'e']);
    expect(pickInsightRows(rows, 2).map((r) => r.id)).toEqual(['a', 'b']);
    expect(pickInsightRows([], 4)).toEqual([]);
  });

  it('never lets expired on shelf be dismissed, and every kind has a look', () => {
    expect(canDismissInsight('expired_on_shelf')).toBe(false);
    expect(canDismissInsight('selling_fast')).toBe(true);
    expect(Object.keys(INSIGHT_LOOK)).toHaveLength(11);
  });
});

describe('DemandInsights', () => {
  it('renders up to four tinted rows, each action described by its sentence', () => {
    render(<DemandInsights {...props({ description: 'From your sales', periodTag: 'Last 7 days' })} />);
    expect(screen.getByRole('region', { name: 'Demand insights' })).toBeInTheDocument();
    expect(screen.getByText('Last 7 days')).toBeInTheDocument();
    const links = screen.getAllByRole('link');
    expect(links.map((l) => l.textContent)).toEqual([
      'Expired action →',
      'Fast action →',
      'Wanted action →',
      'Waste action →',
    ]);
    expect(links[1]).toHaveAccessibleDescription('Fast sentence');
    expect(document.querySelector('[data-kind="selling_fast"]')).toHaveAttribute('data-tint', 'green');
  });

  it('offers Not useful on every row except expired on shelf', () => {
    const onNotUseful = vi.fn();
    render(<DemandInsights {...props({ onNotUseful, notUsefulLabel: 'Not useful' })} />);
    const buttons = screen.getAllByRole('button', { name: /^Not useful/ });
    expect(buttons).toHaveLength(3);
    // Each button names its row, so a list of buttons (or voice control) can tell them apart.
    expect(buttons.map((b) => b.getAttribute('aria-labelledby')?.split(' ').length)).toEqual([2, 2, 2]);
    expect(screen.getByRole('button', { name: 'Not useful Fast' })).toHaveAccessibleDescription('Fast sentence');
    expect(screen.getByRole('button', { name: 'Not useful Wanted' })).toBeInTheDocument();
    fireEvent.click(buttons[0] as HTMLElement);
    expect(onNotUseful).toHaveBeenCalledWith(expect.objectContaining({ id: 'b' }));
  });

  it('shows the empty state with the checklist, the foot links and a custom link component', () => {
    render(
      <DemandInsights
        {...props({
          rows: [],
          empty: { title: 'Nothing needs you right now', checklist: <ul aria-label="Setup" /> },
          allLink: { label: 'All insights', href: '/insights' },
          footNote: 'Updated hourly',
          LinkComponent: StoryLink,
        })}
      />,
    );
    expect(screen.getByText('Nothing needs you right now')).toBeInTheDocument();
    expect(screen.getByRole('list', { name: 'Setup' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'All insights' })).toHaveAttribute('href', '/insights');
    expect(screen.getByText('Updated hourly')).toBeInTheDocument();
  });

  it('shows skeleton rows and aria-busy while loading', () => {
    const { container } = render(<DemandInsights {...props({ loading: true, limit: 3 })} />);
    expect(screen.getByRole('region')).toHaveAttribute('aria-busy', 'true');
    expect(container.querySelectorAll('.tz-insights > *')).toHaveLength(3);
    expect(screen.queryByRole('link')).toBeNull();
  });
});
