import type { Meta, StoryObj } from '@storybook/react-vite';
import {
  ChartColumn,
  Gift,
  House,
  MessageCircle,
  Package,
  Receipt,
  ScanLine,
  Settings,
  Store,
  Tag,
  Users,
} from 'lucide-react';
import { designRef, StoryLink } from '../test/fixtures';
import { Sidebar, type NavGroup } from './Sidebar';

const business: readonly NavGroup[] = [
  {
    key: 'main',
    items: [
      { key: 'overview', label: 'Overview', href: '/', icon: House },
      { key: 'orders', label: 'Orders', href: '/orders', icon: Receipt, count: 3, countLabel: '3 waiting' },
      { key: 'sell', label: 'Sell', href: '/sell', icon: ScanLine },
      { key: 'stock', label: 'Stock', href: '/stock', icon: Package },
      { key: 'customers', label: 'Customers', href: '/customers', icon: Users },
    ],
  },
  {
    key: 'grow',
    label: 'Grow',
    items: [
      { key: 'offers', label: 'Offers', href: '/offers', icon: Tag },
      { key: 'referrals', label: 'Referrals and wallet', href: '/referrals', icon: Gift },
      { key: 'analytics', label: 'Analytics', href: '/analytics', icon: ChartColumn },
    ],
  },
  {
    key: 'shop',
    label: 'Shop',
    items: [
      { key: 'stores', label: 'Stores and staff', href: '/stores', icon: Store },
      { key: 'help', label: 'Help', href: '/help', icon: MessageCircle },
      { key: 'settings', label: 'Settings', href: '/settings', icon: Settings },
    ],
  },
];

const meta = {
  title: 'Navigation/Sidebar',
  component: Sidebar,
  args: { label: 'Business', groups: business, current: 'orders', LinkComponent: StoryLink },
  parameters: { layout: 'fullscreen' },
  decorators: [(Story) => <div style={{ display: 'flex', height: 560 }}>{Story()}</div>],
} satisfies Meta<typeof Sidebar>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Business: Story = {};

export const OverviewCurrent: Story = { args: { current: 'overview' } };

export const WithStoreFooter: Story = {
  args: {
    footer: (
      <div className="tz-side-card">
        <b>Sri Lakshmi Provision Stores</b>
        <small>Jayanagar 4th Block</small>
      </div>
    ),
  },
};

export const DesignReference: Story = {
  ...designRef('Sidebar', 520),
  decorators: [],
  render: (args) => (
    <div style={{ display: 'flex', height: 520 }}>
      <Sidebar {...args} />
    </div>
  ),
};
