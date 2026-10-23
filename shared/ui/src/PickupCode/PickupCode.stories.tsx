import type { Meta, StoryObj } from '@storybook/react-vite';
import { OtpInput } from '../OtpInput/OtpInput';
import { designRef, frame } from '../test/fixtures';
import { PickupCode } from './PickupCode';

const meta = {
  title: 'Commerce/PickupCode',
  component: PickupCode,
  args: { label: 'Pickup code', code: '4827', note: 'Show this at Sri Lakshmi Provision Stores' },
  decorators: [frame({ width: 260 })],
} satisfies Meta<typeof PickupCode>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};

export const WithHoldTimer: Story = {
  args: { children: <span className="tz-ui-code-note">Held for you till 7:40 pm</span> },
};

export const NoNote: Story = { args: { note: undefined } };

export const DesignReference: Story = {
  ...designRef('PickupCode', 300),
  render: () => (
    <div className="tz-row" style={{ alignItems: 'flex-start', gap: 'var(--space-6)' }}>
      <div style={{ width: 260 }}>
        <PickupCode label="Pickup code" code="4827" note="Show this at Sri Lakshmi Provision Stores" />
      </div>
      <OtpInput
        label="Enter customer's code"
        digitLabel={(i) => `Digit ${i}`}
        value="482"
        onChange={() => undefined}
        help="Order TZ-24817 · Ananya R."
      />
    </div>
  ),
};
