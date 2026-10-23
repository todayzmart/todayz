import type { Meta, StoryObj } from '@storybook/react-vite';
import { useState } from 'react';
import { Button } from '../Button/Button';
import { StoreCard } from '../StoreCard/StoreCard';
import { StoryLink } from '../test/fixtures';
import { useToast } from '../Toast/context';
import { ToastProvider } from '../Toast/Toast';
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

function SaveWithUndo(props: SheetProps) {
  const toast = useToast();
  const [open, setOpen] = useState(props.open);
  const [removed, setRemoved] = useState(false);
  const save = () =>
    toast.show({ message: 'Shop saved', action: { label: 'Undo', onClick: () => setRemoved(true) } });
  return (
    <>
      <Button onClick={() => setOpen(true)}>Show shop</Button>
      {removed && <p>Shop removed from saved</p>}
      <Sheet
        {...props}
        open={open}
        onClose={() => setOpen(false)}
        footer={
          <Button variant="primary" block onClick={save}>
            Save shop
          </Button>
        }
      />
    </>
  );
}

/** A toast raised while the sheet is open shows above it and its action can be clicked. */
export const WithToast: Story = {
  args: { open: false },
  render: (args) => (
    <ToastProvider>
      <SaveWithUndo {...args} />
    </ToastProvider>
  ),
};
