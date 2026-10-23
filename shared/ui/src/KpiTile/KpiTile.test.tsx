import { render, screen } from '@testing-library/react';
import { IndianRupee } from 'lucide-react';
import { describe, expect, it } from 'vitest';
import { StoryLink } from '../test/fixtures';
import { KpiStrip, KpiTile } from './KpiTile';

describe('KpiTile', () => {
  it('shows the value and note, and reads the delta in words instead of the arrow', () => {
    const { container } = render(
      <KpiTile
        name="Sales today"
        icon={IndianRupee}
        value="₹18,240"
        note="42 bills"
        delta={{ value: '12%', direction: 'up', label: 'Up 12%' }}
        comparison="vs last Monday"
      />,
    );
    expect(screen.getByText('₹18,240')).toBeInTheDocument();
    expect(screen.getByText('42 bills')).toBeInTheDocument();
    const delta = container.querySelector('.tz-kpi-delta');
    expect(delta?.className).toBe('tz-kpi-delta');
    expect(delta?.querySelector('[aria-hidden="true"]')).toHaveTextContent('▲ 12%');
    expect(delta).toHaveTextContent('Up 12%');
    expect(container.querySelector('.tz-kpi-foot')).toHaveTextContent('vs last Monday');
  });

  it('down and flat deltas take their classes; a link replaces the delta', () => {
    const { container, rerender } = render(
      <KpiTile name="Orders" value="9" delta={{ value: '3', direction: 'down', label: 'Down 3' }} />,
    );
    expect(container.querySelector('.tz-kpi-delta')).toHaveClass('tz-kpi-delta-down');
    rerender(<KpiTile name="Orders" value="9" delta={{ value: '0', direction: 'flat', label: 'No change' }} />);
    expect(container.querySelector('.tz-kpi-delta')).toHaveClass('tz-kpi-delta-flat');
    rerender(
      <KpiTile
        name="Low stock"
        value="6"
        delta={{ value: '0', direction: 'flat', label: 'No change' }}
        link={{ label: 'Reorder list', href: '/stock?low' }}
        LinkComponent={StoryLink}
      />,
    );
    expect(screen.getByRole('link', { name: 'Reorder list' })).toHaveAttribute('href', '/stock?low');
    expect(container.querySelector('.tz-kpi-delta')).toBeNull();
  });

  it('loading shows a skeleton with aria-busy; no foot without delta, comparison or link', () => {
    const { container } = render(<KpiTile name="Sales" value="₹1" loading />);
    expect(container.firstChild).toHaveAttribute('aria-busy', 'true');
    expect(container.querySelector('.tz-ui-skeleton')).not.toBeNull();
    expect(screen.queryByText('₹1')).toBeNull();
    expect(container.querySelector('.tz-kpi-foot')).toBeNull();
  });
});

describe('KpiStrip', () => {
  it('lays out cells with the first as lead, each with its own foot', () => {
    const { container } = render(
      <KpiStrip
        label="Today at a glance"
        items={[
          { key: 'sales', name: 'Sales', icon: IndianRupee, value: '₹18,240', note: '42 bills', comparison: 'vs Mon' },
          { key: 'orders', name: 'Orders', value: '9', delta: { value: '2', direction: 'up', label: 'Up 2' } },
          { key: 'low', name: 'Low stock', value: '6', link: { label: 'Reorder list', href: '/low' } },
          { key: 'items', name: 'Items', value: '1,189', loading: true },
        ]}
      />,
    );
    expect(screen.getByRole('region', { name: 'Today at a glance' })).toBeInTheDocument();
    const cells = container.querySelectorAll('.tz-kpi-cell');
    expect(cells).toHaveLength(4);
    expect(cells[0]).toHaveClass('tz-kpi-cell-lead');
    expect(cells[1]).not.toHaveClass('tz-kpi-cell-lead');
    expect(cells[0]?.querySelector('.tz-kpi-cell-foot')).toHaveTextContent('vs Mon');
    expect(cells[1]?.querySelector('.tz-kpi-delta')).toHaveTextContent('Up 2');
    expect(cells[3]).toHaveAttribute('aria-busy', 'true');
    expect(cells[3]?.querySelector('.tz-kpi-cell-foot')).toBeNull();
    expect(screen.getByRole('link', { name: 'Reorder list' }).tagName).toBe('A');
  });
});
