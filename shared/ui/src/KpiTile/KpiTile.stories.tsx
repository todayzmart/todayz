import type { Meta, StoryObj } from '@storybook/react-vite';
import { IndianRupee, Package, PackageOpen, Receipt } from 'lucide-react';
import { designRef, StoryLink, frame } from '../test/fixtures';
import { KpiStrip, KpiTile, type KpiProps } from './KpiTile';

type Item = KpiProps & { key: string };

const sales: Item = {
  key: 'sales',
  name: 'Sales today',
  icon: IndianRupee,
  value: '₹48,260',
  note: '142 bills · 37 online orders',
  delta: { value: '12%', direction: 'up', label: 'Up 12%' },
  comparison: 'vs last Thursday',
};
const orders: Item = {
  key: 'orders',
  name: 'Orders',
  icon: Receipt,
  value: '37',
  note: '2 waiting to accept',
  delta: { value: '5', direction: 'up', label: 'Up 5' },
  comparison: 'vs last Thu',
};
const items: Item = {
  key: 'items',
  name: 'Items sold',
  icon: Package,
  value: '214',
  note: '86 different items',
  delta: { value: '3%', direction: 'down', label: 'Down 3%' },
  comparison: 'vs last Thu',
};
const low: Item = {
  key: 'low',
  name: 'Low stock',
  icon: PackageOpen,
  value: '9',
  note: '5 sell out by tomorrow',
  link: { label: 'Reorder list', href: '/stock?filter=low' },
};
const today: readonly Item[] = [sales, orders, items, low];

const meta = {
  title: 'Dashboard/KpiTile',
  component: KpiTile,
  args: { ...orders, LinkComponent: StoryLink },
  decorators: [frame({ maxWidth: 260 })],
} satisfies Meta<typeof KpiTile>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Tile: Story = {};

export const Flat: Story = { args: { delta: { value: '0', direction: 'flat', label: 'No change' } } };

export const WithLink: Story = { args: { ...low } };

export const Loading: Story = { args: { loading: true } };

export const Strip: Story = {
  parameters: { frame: false },
  render: () => <KpiStrip label="Today at a glance" items={today} LinkComponent={StoryLink} />,
};

export const StripLoading: Story = {
  parameters: { frame: false },
  render: () => (
    <KpiStrip label="Today at a glance" items={today.map((k) => ({ ...k, loading: true }))} LinkComponent={StoryLink} />
  ),
};

export const DesignReference: Story = {
  ...designRef('KpiTile', 190, 1100),
  render: () => (
    <div style={{ padding: 'var(--space-4)' }}>
      <KpiStrip label="Today at a glance" items={today} LinkComponent={StoryLink} />
    </div>
  ),
};
