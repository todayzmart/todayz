import type { Meta, StoryObj } from '@storybook/react-vite';
import { designRef } from '../test/fixtures';
import { TextField } from './TextField';

const meta = {
  title: 'Inputs and actions/TextField',
  component: TextField,
  args: {
    label: 'Shop name',
    defaultValue: 'Sri Lakshmi Provision Stores',
    help: 'Shown to customers on search results and your shop page.',
  },
  decorators: [(Story) => <div style={{ maxWidth: 420 }}>{Story()}</div>],
} satisfies Meta<typeof TextField>;

export default meta;
type Story = StoryObj<typeof meta>;

export const WithHelp: Story = {};

export const Empty: Story = { args: { defaultValue: '', placeholder: 'e.g. Sri Lakshmi Provision Stores' } };

export const Invalid: Story = {
  args: {
    label: 'PIN code',
    defaultValue: '5600',
    inputMode: 'numeric',
    help: undefined,
    error: 'Enter a 6-digit PIN code.',
  },
};

export const PhonePrefix: Story = {
  args: {
    label: 'Phone number',
    defaultValue: '98450 12345',
    type: 'tel',
    inputMode: 'tel',
    prefix: '+91',
    help: 'We send a one-time code to this number.',
  },
};

export const PhonePrefixInvalid: Story = {
  args: {
    label: 'Phone number',
    defaultValue: '98450',
    type: 'tel',
    prefix: '+91',
    help: undefined,
    error: 'Enter a 10-digit mobile number.',
  },
};

export const Disabled: Story = { args: { disabled: true, help: 'Ask the owner to change the shop name.' } };

export const DesignReference: Story = {
  ...designRef('TextField', 196),
  decorators: [],
  render: () => (
    <div className="tz-stack" style={{ maxWidth: 420 }}>
      <TextField
        id="shop"
        label="Shop name"
        defaultValue="Sri Lakshmi Provision Stores"
        help="Shown to customers on search results and your shop page."
      />
      <TextField id="pin" label="PIN code" inputMode="numeric" defaultValue="5600" error="Enter a 6-digit PIN code." />
    </div>
  ),
};
