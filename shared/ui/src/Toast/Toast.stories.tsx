import type { Meta, StoryObj } from '@storybook/react-vite';
import { RefreshCw, TriangleAlert } from 'lucide-react';
import { Button } from '../Button/Button';
import { useToast } from './context';
import { Toast, ToastProvider } from './Toast';

const meta = {
  title: 'Feedback and overlays/Toast',
  component: Toast,
  args: { message: 'Sale recorded · ₹1,246', action: { label: 'View', href: '#' } },
} satisfies Meta<typeof Toast>;

export default meta;
type Story = StoryObj<typeof meta>;

export const WithLink: Story = {};

export const Plain: Story = { args: { message: 'Stock updated', action: undefined } };

export const WithButton: Story = {
  args: {
    message: 'Could not sync 2 sales',
    icon: TriangleAlert,
    action: { label: 'Retry', onClick: () => undefined },
  },
};

function Triggers() {
  const toast = useToast();
  return (
    <div className="tz-row" style={{ padding: 0 }}>
      <Button onClick={() => toast.show({ message: 'Sale recorded · ₹1,246', action: { label: 'View', href: '#' } })}>
        Record sale
      </Button>
      <Button
        onClick={() =>
          toast.show({
            message: 'A new version is ready',
            icon: RefreshCw,
            duration: null,
            action: { label: 'Reload', onClick: () => undefined },
          })
        }
      >
        App update
      </Button>
    </div>
  );
}

/** The provider's live region: toasts stack above the bottom nav, pause on hover and focus. */
export const Provider: Story = {
  render: () => (
    <ToastProvider>
      <Triggers />
    </ToastProvider>
  ),
};
