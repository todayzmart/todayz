import type { Meta, StoryObj } from '@storybook/react-vite';
import { Plus, Receipt, SearchX } from 'lucide-react';
import { Button } from '../Button/Button';
import { EmptyState } from './EmptyState';

const meta = {
  title: 'Feedback and overlays/EmptyState',
  component: EmptyState,
  args: {
    icon: Receipt,
    title: 'No orders yet',
    description: 'Orders from customers nearby show up here. Keep your stock up to date so they can find you.',
  },
  decorators: [(Story) => <div style={{ maxWidth: 480 }}>{Story()}</div>],
} satisfies Meta<typeof EmptyState>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};

export const WithAction: Story = {
  args: {
    icon: SearchX,
    title: 'No shops nearby have “toor dal”',
    description: 'Try a bigger radius or a shorter word.',
    action: <Button variant="primary">Search 5 km</Button>,
  },
};

export const WithChecklist: Story = {
  args: {
    title: 'Add your first products',
    description: 'Customers can only find what is in your stock.',
    action: (
      <Button variant="primary" icon={Plus}>
        Add product
      </Button>
    ),
    children: (
      <ul>
        <li>Scan a barcode</li>
        <li>Pick from the master catalogue</li>
      </ul>
    ),
  },
};

export const DefaultIcon: Story = { render: () => <EmptyState title="Nothing here yet" headingLevel={2} /> };
