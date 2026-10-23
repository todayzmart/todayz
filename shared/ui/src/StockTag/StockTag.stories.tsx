import type { Meta, StoryObj } from '@storybook/react-vite';
import { designRef } from '../test/fixtures';
import { StockTag } from './StockTag';

const meta = {
  title: 'Status/StockTag',
  component: StockTag,
  args: { level: 'in', label: 'In stock', freshness: 'Confirmed 12 min ago' },
} satisfies Meta<typeof StockTag>;

export default meta;
type Story = StoryObj<typeof meta>;

export const InStock: Story = {};

export const Low: Story = { args: { level: 'low', label: 'Only 3 left', freshness: 'Confirmed 1 hr ago' } };

export const Out: Story = { args: { level: 'out', label: 'Out of stock', freshness: 'Confirmed 3 hr ago' } };

export const Stale: Story = {
  args: { stale: true, staleLabel: 'Stock not confirmed recently' },
};

export const Stacked: Story = { args: { layout: 'stacked' } };

export const NoFreshness: Story = { args: { freshness: undefined } };

/** The card's own wording ("updated …"); product copy follows D-101 ("Confirmed … ago"). */
export const DesignReference: Story = {
  ...designRef('StockTag', 120),
  render: () => (
    <div className="tz-stack">
      <StockTag level="in" label="In stock" freshness="updated 12 min ago" />
      <StockTag level="low" label="Only 3 left" freshness="updated 1 hr ago" />
      <StockTag level="out" label="Out of stock" freshness="updated today, 9:15 am" />
    </div>
  ),
};
