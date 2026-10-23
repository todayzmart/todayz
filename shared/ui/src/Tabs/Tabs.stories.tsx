import type { Meta, StoryObj } from '@storybook/react-vite';
import { useState } from 'react';
import { Tabs, type TabItem } from './Tabs';

type OrderTab = 'new' | 'preparing' | 'ready' | 'done';

const tabs: readonly TabItem<OrderTab>[] = [
  { value: 'new', label: 'New', count: 3, panel: <p>3 orders waiting for you to accept.</p> },
  { value: 'preparing', label: 'Preparing', count: 2, panel: <p>2 orders being packed.</p> },
  { value: 'ready', label: 'Ready', panel: <p>No orders waiting for pickup.</p> },
  { value: 'done', label: 'Done', panel: <p>18 orders completed today.</p> },
];

function OrderTabs({ initial }: { initial: OrderTab }) {
  const [value, setValue] = useState<OrderTab>(initial);
  return <Tabs label="Orders" tabs={tabs} value={value} onChange={setValue} />;
}

const meta = {
  title: 'Inputs and actions/Tabs',
  component: OrderTabs,
  args: { initial: 'new' },
} satisfies Meta<typeof OrderTabs>;

export default meta;
type Story = StoryObj<typeof meta>;

export const WithCounts: Story = {};

export const SecondSelected: Story = { args: { initial: 'ready' } };
