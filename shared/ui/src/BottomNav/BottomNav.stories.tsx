import type { Meta, StoryObj } from '@storybook/react-vite';
import { ChartColumn, Ellipsis, House, Package, Receipt, ScanLine, Search, User, Wallet } from 'lucide-react';
import type { NavItem } from '../Sidebar/Sidebar';
import { designRef, StoryLink } from '../test/fixtures';
import { BottomNav } from './BottomNav';

const customer: readonly NavItem[] = [
  { key: 'home', label: 'Home', href: '/', icon: House },
  { key: 'search', label: 'Search', href: '/search', icon: Search },
  { key: 'orders', label: 'Orders', href: '/orders', icon: Receipt },
  { key: 'wallet', label: 'Wallet', href: '/wallet', icon: Wallet },
  { key: 'account', label: 'Account', href: '/account', icon: User },
];

const business: readonly NavItem[] = [
  { key: 'overview', label: 'Overview', href: '/', icon: ChartColumn },
  { key: 'orders', label: 'Orders', href: '/orders', icon: Receipt },
  { key: 'sell', label: 'Sell', href: '/sell', icon: ScanLine },
  { key: 'stock', label: 'Stock', href: '/stock', icon: Package },
  { key: 'more', label: 'More', href: '/more', icon: Ellipsis },
];

const meta = {
  title: 'Navigation/BottomNav',
  component: BottomNav,
  args: { label: 'Customer', items: customer, current: 'home', LinkComponent: StoryLink },
  decorators: [(Story) => <div style={{ maxWidth: 400 }}>{Story()}</div>],
} satisfies Meta<typeof BottomNav>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Customer: Story = {};

export const Business: Story = { args: { label: 'Business', items: business, current: 'orders' } };

export const WithBadge: Story = {
  args: {
    label: 'Business',
    items: business.map((i) => (i.key === 'orders' ? { ...i, count: 3, countLabel: '3 waiting' } : i)),
    current: 'overview',
  },
};

export const DesignReference: Story = {
  ...designRef('BottomNav', 168),
  decorators: [],
  render: () => (
    <div className="tz-stack" style={{ maxWidth: 400, gap: 'var(--space-4)' }}>
      <BottomNav label="Customer" items={customer} current="home" LinkComponent={StoryLink} />
      <BottomNav label="Business" items={business} current="orders" LinkComponent={StoryLink} />
    </div>
  ),
};
