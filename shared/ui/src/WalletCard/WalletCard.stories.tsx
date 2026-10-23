import type { Meta, StoryObj } from '@storybook/react-vite';
import { Gift } from 'lucide-react';
import { Button } from '../Button/Button';
import { designRef } from '../test/fixtures';
import { WalletCard } from './WalletCard';

const meta = {
  title: 'Commerce/WalletCard',
  component: WalletCard,
  args: {
    label: 'TodayZ points',
    points: '420',
    worth: 'Worth ₹42 off your next order at any TodayZ shop',
    actions: (
      <Button size="sm" icon={Gift}>
        Invite and earn 50
      </Button>
    ),
  },
  decorators: [(Story) => <div style={{ maxWidth: 400 }}>{Story()}</div>],
} satisfies Meta<typeof WalletCard>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};

export const Zero: Story = {
  args: { points: '0', worth: 'Earn points on every order and referral', actions: undefined },
};

export const DesignReference: Story = {
  ...designRef('WalletCard', 230),
  decorators: [],
  render: (args) => (
    <div style={{ padding: 'var(--space-4)', maxWidth: 400 }}>
      <WalletCard {...args} />
    </div>
  ),
};
