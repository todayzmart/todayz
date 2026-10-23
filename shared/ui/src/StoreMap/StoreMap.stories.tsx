import type { Meta, StoryObj } from '@storybook/react-vite';
import { useState } from 'react';
import { designRef, makeShops, mapCenter, radiusOptions } from '../test/fixtures';
import { StoreMap, type StoreMapProps } from './StoreMap';

const center = mapCenter;
const shops = makeShops(43);

const base: StoreMapProps = {
  label: 'Shops on the map',
  mapLabel: 'Map: 43 shops within 2 km, 38 open now',
  center,
  shops,
  radiusKm: 2,
  radiusOptions,
  radiusLabel: 'Search radius',
  onRadiusChange: () => undefined,
  onSelectShop: () => undefined,
  pinLabel: (s) => `${s.name}, ${s.open ? 'open' : 'closed'}`,
  clusterLabel: (n) => `${n} shops, zoom in`,
  youLabel: 'You are here',
  legend: { open: 'Open', closed: 'Closed', you: 'You' },
  liveLabel: 'Live near you',
  stats: [
    { key: 'shops', value: '43', label: 'Shops nearby' },
    { key: 'open', value: '38', label: 'Open now' },
    { key: 'products', value: '1,189', label: 'Products' },
  ],
  expandLabel: 'Open full map',
  onExpand: () => undefined,
  interactive: false,
};

function Stateful(props: StoreMapProps) {
  const [km, setKm] = useState(props.radiusKm);
  const [picked, setPicked] = useState<string | null>(null);
  const name = shops.find((s) => s.id === picked)?.name;
  return (
    <div className="tz-stack" style={{ padding: 0 }}>
      <StoreMap {...props} radiusKm={km} onRadiusChange={setKm} onSelectShop={setPicked} />
      <p className="tz-fresh" aria-live="polite">
        {name ? `Selected: ${name}` : 'Tap a pin to pick a shop'}
      </p>
    </div>
  );
}

const meta = {
  title: 'Map/StoreMap',
  component: StoreMap,
  args: base,
  render: (args) => <Stateful {...args} />,
  decorators: [(Story) => <div style={{ maxWidth: 344 }}>{Story()}</div>],
} satisfies Meta<typeof StoreMap>;

export default meta;
type Story = StoryObj<typeof meta>;

/** MapLibre with the offline fixture style (no network); falls back to static without WebGL. */
export const HomeCard: Story = {};

/** The no-WebGL fallback: land, radius ring and the same pins and clusters. */
export const StaticFallback: Story = { args: { renderer: 'static' } };

export const FullMap: Story = {
  args: { interactive: true, height: 420, onExpand: undefined, expandLabel: undefined },
  decorators: [(Story) => <div style={{ maxWidth: 720 }}>{Story()}</div>],
};

export const NoShops: Story = {
  args: {
    shops: [],
    mapLabel: 'Map: no shops within 2 km',
    stats: [{ key: 'shops', value: '0', label: 'Shops nearby' }],
  },
};

export const DesignReference: Story = {
  ...designRef('StoreMap', 400, 420),
  decorators: [],
  args: { renderer: 'static', attribution: undefined },
  render: (args) => (
    <div style={{ padding: 24, maxWidth: 344 }}>
      <StoreMap {...args} />
    </div>
  ),
};
