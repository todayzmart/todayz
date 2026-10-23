import type { Meta, StoryObj } from '@storybook/react-vite';
import { Mic } from 'lucide-react';
import { useState } from 'react';
import { IconButton } from '../Button/Button';
import { Icon } from '../internal/Icon';
import { designRef, frame } from '../test/fixtures';
import { SearchField, type SearchFieldProps } from './SearchField';

const meta = {
  title: 'Inputs and actions/SearchField',
  component: SearchField,
  args: {
    label: 'Search products and shops',
    placeholder: 'Search atta, milk, medicines…',
    clearLabel: 'Clear search',
  },
  decorators: [frame({ maxWidth: 480 })],
} satisfies Meta<typeof SearchField>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Empty: Story = {};

export const WithQuery: Story = { args: { defaultValue: 'toor dal' } };

export const WithVoice: Story = {
  args: { trailing: <IconButton icon={Mic} label="Search by voice" variant="ghost" size="sm" /> },
};

function Debounced(props: SearchFieldProps) {
  const [query, setQuery] = useState('');
  return (
    <div className="tz-stack" style={{ padding: 0 }}>
      <SearchField {...props} onSearch={setQuery} />
      <p className="tz-fresh" aria-live="polite">
        {query ? `Searching for “${query}”` : 'Type two letters to search'}
      </p>
    </div>
  );
}

export const DebouncedSearch: Story = { render: (args) => <Debounced {...args} /> };

export const DesignReference: Story = {
  ...designRef('SearchField', 88),
  render: () => (
    <div className="tz-stack">
      <SearchField
        id="q"
        label="Search products and shops"
        placeholder="Search atta, milk, medicines…"
        trailing={
          <span style={{ color: 'var(--ink-2)' }}>
            <Icon icon={Mic} />
          </span>
        }
      />
    </div>
  ),
};
