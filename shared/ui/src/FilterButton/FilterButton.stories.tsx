import type { Meta, StoryObj } from '@storybook/react-vite';
import { CalendarDays, ListFilter } from 'lucide-react';
import { FilterButton } from './FilterButton';

const meta = {
  title: 'Inputs and actions/FilterButton',
  component: FilterButton,
  args: { label: 'Status', icon: ListFilter },
} satisfies Meta<typeof FilterButton>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Unset: Story = {};

export const Set: Story = { args: { value: 'Pending' } };

export const Row: Story = {
  render: () => (
    <div className="tz-row" style={{ padding: 0 }}>
      <FilterButton label="Status" icon={ListFilter} value="Pending" />
      <FilterButton label="Date" icon={CalendarDays} value="Today" />
      <FilterButton label="Payment" />
      <FilterButton label="Category" disabled />
    </div>
  ),
};
