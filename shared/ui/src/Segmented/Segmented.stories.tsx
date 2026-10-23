import type { Meta, StoryObj } from '@storybook/react-vite';
import { Bike, ShoppingBag } from 'lucide-react';
import { useState } from 'react';
import { Segmented, type SegmentedOption } from './Segmented';

type Period = 'today' | 'week' | 'month';
type Fulfilment = 'pickup' | 'delivery';

const periods: readonly SegmentedOption<Period>[] = [
  { value: 'today', label: 'Today' },
  { value: 'week', label: 'This week' },
  { value: 'month', label: 'This month' },
];

const fulfilment: readonly SegmentedOption<Fulfilment>[] = [
  { value: 'pickup', label: 'Pickup', icon: ShoppingBag },
  { value: 'delivery', label: 'Delivery', icon: Bike },
];

function PeriodPicker({ initial }: { initial: Period }) {
  const [value, setValue] = useState<Period>(initial);
  return <Segmented label="Period" options={periods} value={value} onChange={setValue} />;
}

function FulfilmentPicker() {
  const [value, setValue] = useState<Fulfilment>('pickup');
  return <Segmented label="How you get it" options={fulfilment} value={value} onChange={setValue} />;
}

const meta = {
  title: 'Inputs and actions/Segmented',
  component: PeriodPicker,
  args: { initial: 'today' },
} satisfies Meta<typeof PeriodPicker>;

export default meta;
type Story = StoryObj<typeof meta>;

export const TextOnly: Story = {};

export const LastSelected: Story = { args: { initial: 'month' } };

export const WithIcons: Story = { render: () => <FulfilmentPicker /> };
