import type { Meta, StoryObj } from '@storybook/react-vite';
import { Button } from '../Button/Button';
import { designRef, frame } from '../test/fixtures';
import { Toast } from '../Toast/Toast';
import { Banner } from './Banner';

const meta = {
  title: 'Feedback and overlays/Banner',
  component: Banner,
  args: { title: "You're offline.", children: "Sales you record now will sync when you're back online." },
  decorators: [frame({ maxWidth: 560 })],
} satisfies Meta<typeof Banner>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Info: Story = {};

export const Warning: Story = {
  args: {
    tone: 'warning',
    title: 'WhatsApp is off.',
    children: 'Order updates go out as in-app and SMS only. Add a WhatsApp API key in Settings to turn it on.',
  },
};

export const WithAction: Story = {
  args: {
    tone: 'warning',
    title: 'A new version is ready.',
    children: 'Reload to get the latest fixes.',
    action: <Button size="sm">Reload</Button>,
  },
};

export const LiveStatus: Story = { args: { live: 'status' } };

export const DesignReference: Story = {
  ...designRef('Banner', 210),
  render: () => (
    <div className="tz-stack" style={{ maxWidth: 560 }}>
      <Banner tone="warning" title="WhatsApp is off.">
        Order updates go out as in-app and SMS only. Add a WhatsApp API key in Settings to turn it on.
      </Banner>
      <Banner title="You're offline.">Sales you record now will sync when you're back online.</Banner>
      <div>
        <Toast message="Sale recorded · ₹1,246" action={{ label: 'View', href: '#' }} />
      </div>
    </div>
  ),
};
