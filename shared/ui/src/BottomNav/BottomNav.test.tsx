import { render, screen } from '@testing-library/react';
import { Boxes, LayoutDashboard, ReceiptText } from 'lucide-react';
import { describe, expect, it } from 'vitest';
import { StoryLink } from '../test/fixtures';
import { BottomNav } from './BottomNav';

describe('BottomNav', () => {
  it('marks the current tab, caps the badge at 99+ and reads the count in words', () => {
    render(
      <BottomNav
        label="Business"
        current="orders"
        LinkComponent={StoryLink}
        items={[
          { key: 'home', label: 'Home', href: '/', icon: LayoutDashboard },
          { key: 'orders', label: 'Orders', href: '/orders', icon: ReceiptText, count: 120, countLabel: '120 new' },
          { key: 'stock', label: 'Stock', href: '/stock', icon: Boxes, count: 4 },
        ]}
      />,
    );
    expect(screen.getByRole('navigation', { name: 'Business' })).toBeInTheDocument();
    const orders = screen.getByRole('link', { name: /^Orders/ });
    expect(orders).toHaveAttribute('aria-current', 'page');
    expect(orders.querySelector('.tz-ui-bnav-badge')).toHaveTextContent('99+');
    expect(orders.querySelector('.tz-ui-bnav-badge')).toHaveAttribute('aria-hidden', 'true');
    expect(orders).toHaveAccessibleName(/120 new$/);
    expect(screen.getByRole('link', { name: /^Stock/ })).toHaveTextContent('4Stock, 4');
    const home = screen.getByRole('link', { name: 'Home' });
    expect(home).not.toHaveAttribute('aria-current');
    expect(home.querySelector('.tz-ui-bnav-badge')).toBeNull();
  });

  it('uses plain anchors by default and hides a zero count', () => {
    render(
      <BottomNav
        label="Customer"
        items={[{ key: 'home', label: 'Home', href: '/', icon: LayoutDashboard, count: 0 }]}
      />,
    );
    const home = screen.getByRole('link', { name: 'Home' });
    expect(home.tagName).toBe('A');
    expect(home.querySelector('.tz-ui-bnav-badge')).toBeNull();
  });
});
