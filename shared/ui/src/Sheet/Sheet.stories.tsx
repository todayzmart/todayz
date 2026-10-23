import type { Meta, StoryObj } from '@storybook/react-vite';
import { useState } from 'react';
import { Button } from '../Button/Button';
import { StoreCard } from '../StoreCard/StoreCard';
import { StoryLink } from '../test/fixtures';
import { Sheet, type SheetProps } from './Sheet';

function Launcher(props: SheetProps) {
  const [open, setOpen] = useState(props.open);
  return (
    <>
      <Button onClick={() => setOpen(true)}>Show shop</Button>
      <Sheet {...props} open={open} onClose={() => setOpen(false)} />
    </>
  );
}

const meta = {
  title: 'Feedback and overlays/Sheet',
  component: Sheet,
  args: {
    open: true,
    onClose: () => undefined,
    title: 'Sri Lakshmi Provision Stores',
    description: '0.6 km · Jayanagar 4th Block · open till 10 pm',
    closeLabel: 'Close',
    children: (
      <StoreCard
        name="Sri Lakshmi Provision Stores"
        initials="SL"
        meta={['0.6 km', 'Jayanagar 4th Block']}
        tags={[{ label: 'Open till 10 pm', kind: 'open' }, { label: 'Pickup' }]}
        href="/s/sri-lakshmi-provision-stores"
        LinkComponent={StoryLink}
      />
    ),
    footer: (
      <Button variant="primary" block>
        View shop
      </Button>
    ),
  },
  render: (args) => <Launcher {...args} />,
  parameters: { layout: 'fullscreen' },
} satisfies Meta<typeof Sheet>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Open: Story = {};

export const Closed: Story = { args: { open: false } };

export const NotDismissible: Story = {
  args: {
    title: 'Pick a payment method',
    description: 'Choose how the customer paid before you close the bill.',
    closeLabel: undefined,
    dismissible: false,
    children: undefined,
  },
};
