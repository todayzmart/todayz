import type { Meta, StoryObj } from '@storybook/react-vite';
import { designRef } from '../test/fixtures';
import { orderStatusTone, type OrderStatus } from './orderStatus';
import { StatusPill } from './StatusPill';

const meta = {
  title: 'Status/StatusPill',
  component: StatusPill,
  args: { tone: 'ready', children: 'Ready for pickup' },
  argTypes: {
    tone: {
      control: 'select',
      options: [
        'neutral',
        'pending',
        'accepted',
        'preparing',
        'ready',
        'delivery',
        'completed',
        'rejected',
        'cancelled',
        'paid',
      ],
    },
  },
} satisfies Meta<typeof StatusPill>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Playground: Story = {};

const orderLabels: Record<OrderStatus, string> = {
  placed: 'Pending',
  awaiting_customer: 'Waiting for you',
  accepted: 'Accepted',
  ready: 'Ready for pickup',
  out_for_delivery: 'Out for delivery',
  collected: 'Collected',
  delivered: 'Delivered',
  rejected: 'Rejected',
  expired: 'Expired',
  cancelled: 'Cancelled',
  delivery_failed: 'Delivery failed',
};

/** Every order status in the data model with the tone orderStatusTone gives it. */
export const EveryOrderStatus: Story = {
  render: () => (
    <div className="tz-row" style={{ padding: 0 }}>
      {(Object.keys(orderLabels) as OrderStatus[]).map((s) => (
        <StatusPill key={s} tone={orderStatusTone(s)}>
          {orderLabels[s]}
        </StatusPill>
      ))}
    </div>
  ),
};

export const DesignReference: Story = {
  ...designRef('StatusPill', 120),
  render: () => (
    <div className="tz-stack">
      <div className="tz-row" style={{ padding: 0 }}>
        <StatusPill tone="pending">Pending</StatusPill>
        <StatusPill tone="accepted">Accepted</StatusPill>
        <StatusPill tone="preparing">Preparing</StatusPill>
        <StatusPill tone="ready">Ready for pickup</StatusPill>
        <StatusPill tone="delivery">Out for delivery</StatusPill>
        <StatusPill tone="completed">Completed</StatusPill>
        <StatusPill tone="rejected">Rejected</StatusPill>
        <StatusPill tone="cancelled">Cancelled</StatusPill>
      </div>
      <div className="tz-row" style={{ padding: 0 }}>
        <StatusPill>Unpaid · pay at pickup</StatusPill>
        <StatusPill tone="paid">Paid · UPI</StatusPill>
      </div>
    </div>
  ),
};
