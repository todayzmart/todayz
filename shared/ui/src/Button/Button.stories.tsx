import type { Meta, StoryObj } from '@storybook/react-vite';
import { Download, Ellipsis, MapPin, Mic, Plus } from 'lucide-react';
import { designRef } from '../test/fixtures';
import { Button, IconButton } from './Button';

const meta = {
  title: 'Inputs and actions/Button',
  component: Button,
  args: { children: 'Accept order', variant: 'primary' },
  argTypes: {
    variant: { control: 'select', options: ['default', 'primary', 'brand', 'ghost', 'danger', 'soft', 'soft-danger'] },
    size: { control: 'inline-radio', options: ['sm', 'md', 'lg'] },
  },
} satisfies Meta<typeof Button>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Playground: Story = {};

export const Variants: Story = {
  render: () => (
    <div className="tz-row" style={{ padding: 0 }}>
      <Button variant="primary">Accept order</Button>
      <Button variant="brand">Place order</Button>
      <Button icon={MapPin}>Directions</Button>
      <Button variant="ghost">View all</Button>
      <Button variant="danger">Reject order</Button>
      <Button variant="soft">Change</Button>
      <Button variant="soft-danger">Remove</Button>
    </div>
  ),
};

export const Sizes: Story = {
  render: () => (
    <div className="tz-row" style={{ padding: 0 }}>
      <Button variant="primary" size="sm">
        Accept
      </Button>
      <Button variant="primary">Accept order</Button>
      <Button variant="primary" size="lg">
        Save changes
      </Button>
    </div>
  ),
};

export const Disabled: Story = { args: { variant: 'default', children: 'Out of stock', disabled: true } };

export const Loading: Story = { args: { variant: 'brand', size: 'lg', children: 'Charge ₹1,246', loading: true } };

export const Block: Story = { args: { variant: 'brand', size: 'lg', block: true, children: 'Place order' } };

export const WithIcon: Story = { args: { variant: 'default', icon: Plus, children: 'Add product' } };

export const IconOnly: Story = {
  render: () => (
    <div className="tz-row" style={{ padding: 0 }}>
      <IconButton icon={Mic} label="Voice update" />
      <IconButton icon={Ellipsis} label="More" size="sm" />
      <IconButton icon={Download} label="Export CSV" variant="ghost" />
    </div>
  ),
};

export const DesignReference: Story = {
  ...designRef('Button', 200),
  render: () => (
    <div className="tz-stack">
      <div className="tz-row" style={{ padding: 0 }}>
        <Button variant="primary">Accept order</Button>
        <Button variant="brand">Place order</Button>
        <Button icon={MapPin}>Directions</Button>
        <Button variant="ghost">View all</Button>
        <Button variant="danger">Reject order</Button>
        <Button disabled>Out of stock</Button>
        <IconButton icon={Mic} label="Voice update" />
      </div>
      <div className="tz-row" style={{ padding: 0 }}>
        <Button variant="primary" size="lg">
          Save changes
        </Button>
        <Button variant="brand" size="lg">
          Charge ₹1,246
        </Button>
        <Button size="sm" icon={Download}>
          Export CSV
        </Button>
      </div>
      <div className="tz-row" style={{ padding: 0 }}>
        <Button size="sm" variant="soft">
          Change
        </Button>
        <Button size="sm" variant="soft-danger">
          Remove
        </Button>
        <Button size="sm" variant="ghost">
          Cancel
        </Button>
        <IconButton size="sm" icon={Ellipsis} label="More" />
        <Button size="sm" variant="primary">
          Accept
        </Button>
      </div>
    </div>
  ),
};
