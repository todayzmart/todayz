import type { Meta, StoryObj } from '@storybook/react-vite';
import { designRef, frame } from '../test/fixtures';
import { CartBar } from './CartBar';

const meta = {
  title: 'Commerce/CartBar',
  component: CartBar,
  args: {
    count: 3,
    countLabel: '3 items',
    total: '₹345',
    from: 'from Sri Lakshmi Provision Stores',
    ctaLabel: 'View cart',
    onOpen: () => undefined,
  },
  decorators: [frame({ maxWidth: 400 })],
} satisfies Meta<typeof CartBar>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};

export const OneItem: Story = { args: { count: 1, countLabel: '1 item', total: '₹28' } };

export const LargeCart: Story = {
  args: {
    count: 24,
    countLabel: '24 items',
    total: '₹2,418',
    from: 'from Sri Venkateshwara Medical and General Stores',
  },
};

export const NoShop: Story = { args: { from: undefined } };

export const DesignReference: Story = {
  ...designRef('CartBar', 96),
  render: (args) => (
    <div className="tz-stack" style={{ maxWidth: 400 }}>
      <CartBar {...args} />
    </div>
  ),
};
