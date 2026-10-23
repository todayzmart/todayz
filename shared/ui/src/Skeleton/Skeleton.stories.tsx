import type { Meta, StoryObj } from '@storybook/react-vite';
import { Skeleton } from './Skeleton';

const meta = {
  title: 'Feedback and overlays/Skeleton',
  component: Skeleton,
  args: { width: '60%', shape: 'text' },
} satisfies Meta<typeof Skeleton>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Text: Story = {};

export const Block: Story = { args: { width: '100%', height: '120px', shape: 'block' } };

export const Circle: Story = { args: { width: '44px', height: '44px', shape: 'circle' } };

/** A store card while loading: the region carries aria-busy, the skeletons are decorative. */
export const StoreCardLoading: Story = {
  render: () => (
    <div className="tz-scard" aria-busy="true" aria-label="Loading shops" role="group" style={{ maxWidth: 420 }}>
      <Skeleton width="44px" height="44px" shape="circle" />
      <div className="tz-stack" style={{ padding: 0, gap: 8, flex: 1 }}>
        <Skeleton width="70%" />
        <Skeleton width="45%" />
      </div>
    </div>
  ),
};
