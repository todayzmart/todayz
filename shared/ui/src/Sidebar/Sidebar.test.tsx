import { render, screen } from '@testing-library/react';
import { Boxes, LayoutDashboard, ReceiptText } from 'lucide-react';
import { describe, expect, it } from 'vitest';
import { StoryLink } from '../test/fixtures';
import { Sidebar, type NavGroup } from './Sidebar';

const groups: NavGroup[] = [
  {
    key: 'shop',
    items: [
      { key: 'overview', label: 'Overview', href: '/', icon: LayoutDashboard },
      { key: 'orders', label: 'Orders', href: '/orders', icon: ReceiptText, count: 3, countLabel: '3 waiting' },
      { key: 'stock', label: 'Stock', href: '/stock', icon: Boxes, count: 0 },
    ],
  },
  {
    key: 'grow',
    label: 'Grow',
    items: [{ key: 'sales', label: 'Sales', href: '/sales', icon: ReceiptText, count: 5 }],
  },
];

describe('Sidebar', () => {
  it('shows the wordmark, group headings, the current page and counts read in words', () => {
    render(<Sidebar label="Business" groups={groups} current="orders" />);
    expect(screen.getByRole('navigation', { name: 'Business' })).toHaveTextContent(/^TodayZ/);
    expect(screen.getByText('Grow')).toHaveClass('tz-side-group');
    const orders = screen.getByRole('link', { name: /^Orders/ });
    expect(orders).toHaveAttribute('aria-current', 'page');
    expect(orders).toHaveAccessibleName(/3 waiting$/);
    expect(orders.querySelector('.tz-count')).toHaveAttribute('aria-hidden', 'true');
    const stock = screen.getByRole('link', { name: 'Stock' });
    expect(stock).not.toHaveAttribute('aria-current');
    expect(stock.querySelector('.tz-count')).toBeNull();
    expect(screen.getByRole('link', { name: /^Sales/ })).toHaveTextContent('Sales5, 5');
    expect(screen.getByRole('link', { name: 'Overview' }).tagName).toBe('A');
  });

  it('takes a brand, footer and link component', () => {
    render(
      <Sidebar
        label="Business"
        groups={groups}
        brand={<b>Sri Lakshmi</b>}
        footer={<p>Signed in</p>}
        LinkComponent={StoryLink}
      />,
    );
    expect(screen.getByText('Sri Lakshmi')).toBeInTheDocument();
    expect(screen.queryByText('Today')).toBeNull();
    expect(screen.getByText('Signed in')).toBeInTheDocument();
  });
});
