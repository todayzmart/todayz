import type { Meta, StoryObj } from '@storybook/react-vite';
import { useState } from 'react';
import { Button } from '../Button/Button';
import { Dialog, type DialogProps } from './Dialog';

function Launcher(props: DialogProps) {
  const [open, setOpen] = useState(props.open);
  const close = () => setOpen(false);
  return (
    <>
      <Button variant="danger" onClick={() => setOpen(true)}>
        Reject order
      </Button>
      <Dialog
        {...props}
        open={open}
        onClose={close}
        footer={
          <>
            <Button variant="ghost" onClick={close}>
              Keep order
            </Button>
            <Button variant="danger" onClick={close}>
              Reject order
            </Button>
          </>
        }
      />
    </>
  );
}

const meta = {
  title: 'Feedback and overlays/Dialog',
  component: Dialog,
  args: {
    open: true,
    onClose: () => undefined,
    role: 'alertdialog',
    title: 'Reject order TZ-24817?',
    description: 'Ananya R. is told the shop could not take the order. This cannot be undone.',
  },
  render: (args) => <Launcher {...args} />,
  parameters: { layout: 'fullscreen' },
} satisfies Meta<typeof Dialog>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Confirm: Story = {};

export const WithCloseButton: Story = {
  args: {
    role: 'dialog',
    title: 'Opening hours',
    description: 'Customers see these on your shop page.',
    closeLabel: 'Close',
    children: <p>Monday to Saturday, 7 am to 10 pm. Sunday, 8 am to 1 pm.</p>,
  },
};

export const Closed: Story = { args: { open: false } };
