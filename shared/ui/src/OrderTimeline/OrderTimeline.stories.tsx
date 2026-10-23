import type { Meta, StoryObj } from '@storybook/react-vite';
import { ShoppingBag } from 'lucide-react';
import { designRef } from '../test/fixtures';
import { OrderTimeline, type TimelineStep } from './OrderTimeline';

const stateLabels = { done: 'done', upcoming: 'not yet', failed: 'failed' };

const ready: readonly TimelineStep[] = [
  { key: 'placed', label: 'Order placed', state: 'done', detail: '6:02 pm' },
  { key: 'accepted', label: 'Accepted by the shop', state: 'done', detail: '6:04 pm' },
  { key: 'preparing', label: 'Preparing', state: 'done', detail: '6:05 pm' },
  {
    key: 'ready',
    label: 'Ready for pickup',
    state: 'now',
    detail: '6:18 pm · show your code at the counter',
    icon: ShoppingBag,
  },
  { key: 'collected', label: 'Collected', state: 'upcoming' },
];

const meta = {
  title: 'Commerce/OrderTimeline',
  component: OrderTimeline,
  args: { label: 'Order progress', steps: ready, stateLabels },
  decorators: [(Story) => <div style={{ maxWidth: 360 }}>{Story()}</div>],
} satisfies Meta<typeof OrderTimeline>;

export default meta;
type Story = StoryObj<typeof meta>;

export const ReadyForPickup: Story = {};

export const JustPlaced: Story = {
  args: {
    steps: [
      { key: 'placed', label: 'Order placed', state: 'now', detail: '6:02 pm · waiting for the shop to accept' },
      { key: 'accepted', label: 'Accepted by the shop', state: 'upcoming' },
      { key: 'ready', label: 'Ready for pickup', state: 'upcoming' },
      { key: 'collected', label: 'Collected', state: 'upcoming' },
    ],
  },
};

export const Completed: Story = {
  args: {
    steps: ready.map((s) => ({ ...s, state: 'done' as const, detail: s.key === 'collected' ? '6:31 pm' : s.detail })),
  },
};

export const Rejected: Story = {
  args: {
    steps: [
      { key: 'placed', label: 'Order placed', state: 'done', detail: '6:02 pm' },
      {
        key: 'rejected',
        label: 'Rejected by the shop',
        state: 'failed',
        detail: '6:09 pm · Fortune Sunflower Oil is out of stock',
      },
    ],
  },
};

export const DesignReference: Story = {
  ...designRef('OrderTimeline', 300),
  decorators: [],
  render: (args) => (
    <div className="tz-stack" style={{ maxWidth: 360 }}>
      <OrderTimeline {...args} />
    </div>
  ),
};
